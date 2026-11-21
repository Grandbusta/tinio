package connect

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/Grandbusta/tinio/internal/ability"
	"github.com/Grandbusta/tinio/internal/action"
	"github.com/Grandbusta/tinio/internal/mind"
	"github.com/Grandbusta/tinio/internal/tools"
)

// Google's tools, Tinio's own. Calendar reads events and adds ones with no
// guests; Drive reads only the files the owner picked and the docs agents
// made in a "Tinio" folder, and makes and adds to docs there. Nothing
// invites, shares, moves or deletes.

const (
	calendarAPI = "https://www.googleapis.com/calendar/v3/calendars/primary/events"
	driveAPI    = "https://www.googleapis.com/drive/v3/files"
	driveUpload = "https://www.googleapis.com/upload/drive/v3/files"
	googleDoc   = "application/vnd.google-apps.document"
	folderMime  = "application/vnd.google-apps.folder"
	readAtMost  = 12000 // runes of a file or a list they read
)

type google struct {
	given  Given
	client *http.Client
	saver  Saver
	store  tools.Store
	limit  int
}

func (g *google) reads() []ability.Ability {
	var out []ability.Ability
	if g.given.Access.Has("calendar") {
		out = append(out, gread{g: g, name: "google.calendar", about: `Read the owner's Google Calendar: their events and when they're free. Query: {} for the next 7 days, or {"from": "YYYY-MM-DD", "to": "YYYY-MM-DD"}.`, read: g.events})
	}
	if g.given.Access.Has("drive") {
		out = append(out, gread{g: g, name: "google.files", about: `Read the owner's Google Drive files they picked for you, and the docs you made in their "Tinio" folder. Query: {} to list them, or {"file": "its name or id"} to read one.`, read: g.files})
	}
	return out
}

func (g *google) doers() []mind.Doer {
	var out []mind.Doer
	if g.given.Access.Has("calendar_add") {
		out = append(out, gdo{g: g, name: "google.add_event", about: `Add an event to the owner's Google Calendar, with no guests. Arguments: {"title": "...", "start": "RFC 3339 time with offset", "end": "RFC 3339 time with offset", "about": "optional notes"}.`, check: checkEvent, do: g.addEvent})
	}
	if g.given.Access.Has("drive") {
		out = append(out,
			gdo{g: g, name: "google.make_doc", about: `Make a Google Doc in the owner's "Tinio" folder. Arguments: {"title": "...", "text": "its words"}.`, check: checkDoc, do: g.makeDoc},
			gdo{g: g, name: "google.add_to_doc", about: `Add words to the end of a doc you made in the owner's "Tinio" folder. Arguments: {"file": "its name or id", "text": "the words to add"}.`, check: checkAdd, do: g.addToDoc})
	}
	return out
}

// gread is a Google reading tool, offered as an ability.
type gread struct {
	g     *google
	name  string
	about string
	read  func(ctx context.Context, a ability.Act, args map[string]string) (string, error)
}

func (r gread) Name() string       { return r.name }
func (r gread) Describe() string   { return r.about }
func (r gread) Risk() ability.Risk { return ability.UsesTools }

func (r gread) Prepare(ctx context.Context, a ability.Act) (ability.Material, error) {
	args := map[string]string{}
	if q := strings.TrimSpace(a.Query()); q != "" && q != "{}" {
		if err := json.Unmarshal([]byte(q), &args); err != nil {
			return ability.Material{}, fmt.Errorf("%s: its query isn't a JSON object of strings: %w", r.name, err)
		}
	}
	if err := tools.Under(ctx, r.g.store, a, r.g.limit); err != nil {
		return ability.Material{}, err
	}
	text, err := r.read(ctx, a, args)
	if logErr := tools.Log(ctx, r.g.store, r.name, a.Query(), a.Now); logErr != nil {
		return ability.Material{}, logErr
	}
	if err != nil {
		return ability.Material{}, err
	}
	note := fmt.Sprintf("\n\nYou used %s (Google, %s) with %s; it returned this.%s", r.name, r.g.given.Label, orEmpty(a.Query()), ability.Fence(text))
	return ability.Material{Note: note, Project: a.Project}, nil
}

// gdo is a Google tool that changes something: only ever proposed, and run
// once the gate (write) allows it.
type gdo struct {
	g     *google
	name  string
	about string
	check func(args map[string]string) error
	do    func(ctx context.Context, args map[string]string) (string, error)
}

func (d gdo) Name() string        { return d.name }
func (d gdo) Describe() string    { return d.about }
func (d gdo) Class() action.Class { return action.Write }
func (d gdo) Destructive() bool   { return false } // they only add

func (d gdo) Check(raw string) error {
	args, err := strArgs(raw)
	if err != nil {
		return fmt.Errorf("%s: %w", d.name, err)
	}
	return d.check(args)
}

func (d gdo) Do(ctx context.Context, raw string, at time.Time) (string, error) {
	args, err := strArgs(raw)
	if err == nil {
		err = d.check(args)
	}
	if err != nil {
		return "", fmt.Errorf("%s: %w", d.name, err)
	}
	text, err := d.do(ctx, args)
	if logErr := d.g.store.LogWeb(ctx, "action", d.name+" "+cutRunes(raw, 500), 0, at.UTC()); logErr != nil {
		return "", logErr
	}
	return text, err
}

func strArgs(raw string) (map[string]string, error) {
	args := map[string]string{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &args); err != nil {
		return nil, fmt.Errorf("its arguments aren't a JSON object of strings: %w", err)
	}
	return args, nil
}

// --- Calendar ---

func (g *google) events(ctx context.Context, a ability.Act, args map[string]string) (string, error) {
	zone := a.Zone
	if zone == nil {
		zone = time.UTC
	}
	l := a.Now.In(zone)
	from := time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, zone)
	to := from.AddDate(0, 0, 7)
	if s := args["from"]; s != "" {
		d, err := time.ParseInLocation("2006-01-02", s, zone)
		if err != nil {
			return "", fmt.Errorf(`from must be "YYYY-MM-DD"`)
		}
		from, to = d, d.AddDate(0, 0, 1)
	}
	if s := args["to"]; s != "" {
		d, err := time.ParseInLocation("2006-01-02", s, zone)
		if err != nil || d.Before(from) {
			return "", fmt.Errorf(`to must be "YYYY-MM-DD", on or after from`)
		}
		to = d.AddDate(0, 0, 1)
	}
	if to.Sub(from) > 62*24*time.Hour {
		to = from.AddDate(0, 0, 62)
	}
	q := url.Values{"timeMin": {from.Format(time.RFC3339)}, "timeMax": {to.Format(time.RFC3339)}, "singleEvents": {"true"},
		"orderBy": {"startTime"}, "maxResults": {"100"}, "timeZone": {zone.String()}}
	var out struct {
		Items []struct {
			Summary   string `json:"summary"`
			Location  string `json:"location"`
			Start     when   `json:"start"`
			End       when   `json:"end"`
			Attendees []any  `json:"attendees"`
		} `json:"items"`
	}
	if err := g.call(ctx, http.MethodGet, calendarAPI+"?"+q.Encode(), nil, "", &out); err != nil {
		return "", err
	}
	if len(out.Items) == 0 {
		return fmt.Sprintf("Nothing from %s to %s.", from.Format("Mon 2 Jan"), to.AddDate(0, 0, -1).Format("Mon 2 Jan")), nil
	}
	var b strings.Builder
	for _, e := range out.Items {
		fmt.Fprintf(&b, "- %s: %s", e.Start.show(zone, e.End), orDefault(e.Summary, "(no title)"))
		if e.Location != "" {
			fmt.Fprintf(&b, ", at %s", e.Location)
		}
		if n := len(e.Attendees); n > 1 {
			fmt.Fprintf(&b, ", with %d others", n-1)
		}
		b.WriteString("\n")
	}
	return cutRunes(b.String(), readAtMost), nil
}

type when struct {
	DateTime string `json:"dateTime"`
	Date     string `json:"date"`
}

func (w when) show(zone *time.Location, end when) string {
	if w.Date != "" {
		d, _ := time.Parse("2006-01-02", w.Date)
		return d.Format("Mon 2 Jan") + ", all day"
	}
	s, err1 := time.Parse(time.RFC3339, w.DateTime)
	e, err2 := time.Parse(time.RFC3339, end.DateTime)
	if err1 != nil || err2 != nil {
		return w.DateTime
	}
	return s.In(zone).Format("Mon 2 Jan 15:04") + " to " + e.In(zone).Format("15:04")
}

func checkEvent(args map[string]string) error {
	start, err1 := time.Parse(time.RFC3339, args["start"])
	end, err2 := time.Parse(time.RFC3339, args["end"])
	switch {
	case strings.TrimSpace(args["title"]) == "":
		return errors.New("it needs a title")
	case err1 != nil || err2 != nil:
		return errors.New("start and end must be RFC 3339 times with an offset, like 2026-11-27T15:00:00+00:00")
	case !end.After(start) || end.Sub(start) > 7*24*time.Hour:
		return errors.New("end must be after start, and within a week of it")
	}
	return nil
}

func (g *google) addEvent(ctx context.Context, args map[string]string) (string, error) {
	body := map[string]any{"summary": cutRunes(args["title"], 200), "description": cutRunes(args["about"], 4000),
		"start": map[string]string{"dateTime": args["start"]}, "end": map[string]string{"dateTime": args["end"]}}
	raw, _ := json.Marshal(body)
	var out struct {
		Link string `json:"htmlLink"`
	}
	if err := g.call(ctx, http.MethodPost, calendarAPI+"?sendUpdates=none", bytes.NewReader(raw), "application/json", &out); err != nil {
		return "", err
	}
	return fmt.Sprintf("Added %q to the owner's Google Calendar: %s", args["title"], out.Link), nil
}

// --- Drive and Docs ---

type driveFile struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Mime    string   `json:"mimeType"`
	Parents []string `json:"parents"`
	Link    string   `json:"webViewLink"`
}

// made is the docs agents made in the "Tinio" folder.
func (g *google) made(ctx context.Context) ([]driveFile, error) {
	if g.given.Access.Folder == "" {
		return nil, nil
	}
	q := url.Values{"q": {fmt.Sprintf("'%s' in parents and trashed = false", g.given.Access.Folder)}, "fields": {"files(id,name,mimeType)"}, "pageSize": {"100"}}
	var out struct {
		Files []driveFile `json:"files"`
	}
	if err := g.call(ctx, http.MethodGet, driveAPI+"?"+q.Encode(), nil, "", &out); err != nil {
		return nil, err
	}
	return out.Files, nil
}

// find is the picked file or made doc named or numbered ref; ok false if
// it's neither: nothing else in their Drive is theirs to read.
func (g *google) find(ctx context.Context, ref string, madeOnly bool) (driveFile, bool, error) {
	ref = strings.TrimSpace(ref)
	var all []driveFile
	if !madeOnly {
		for _, f := range g.given.Access.Files {
			all = append(all, driveFile{ID: f.ID, Name: f.Name, Mime: f.Mime})
		}
	}
	made, err := g.made(ctx)
	if err != nil {
		return driveFile{}, false, err
	}
	all = append(all, made...)
	i := slices.IndexFunc(all, func(f driveFile) bool { return f.ID == ref || strings.EqualFold(f.Name, ref) })
	if i < 0 {
		return driveFile{}, false, nil
	}
	return all[i], true, nil
}

func (g *google) files(ctx context.Context, _ ability.Act, args map[string]string) (string, error) {
	if args["file"] == "" {
		made, err := g.made(ctx)
		if err != nil {
			return "", err
		}
		var b strings.Builder
		b.WriteString("Files the owner picked for you:\n")
		for _, f := range g.given.Access.Files {
			fmt.Fprintf(&b, "- %s (%s)\n", f.Name, kindOf(f.Mime))
		}
		if len(g.given.Access.Files) == 0 {
			b.WriteString("- none yet\n")
		}
		b.WriteString("Docs you made in their Tinio folder:\n")
		for _, f := range made {
			fmt.Fprintf(&b, "- %s\n", f.Name)
		}
		if len(made) == 0 {
			b.WriteString("- none yet\n")
		}
		return b.String(), nil
	}
	f, ok, err := g.find(ctx, args["file"], false)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("%q isn't a file the owner picked or a doc you made; ask them to pick it in Tinio", args["file"])
	}
	text, err := g.text(ctx, f)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s (%s):\n%s", f.Name, kindOf(f.Mime), cutRunes(text, readAtMost)), nil
}

// text is a file's words: a Google Doc or Slides as text, a Sheet as CSV, a
// text file as it is; anything else is only described.
func (g *google) text(ctx context.Context, f driveFile) (string, error) {
	export := map[string]string{googleDoc: "text/plain", "application/vnd.google-apps.spreadsheet": "text/csv",
		"application/vnd.google-apps.presentation": "text/plain"}[f.Mime]
	u := driveAPI + "/" + url.PathEscape(f.ID)
	switch {
	case export != "":
		u += "/export?mimeType=" + url.QueryEscape(export)
	case strings.HasPrefix(f.Mime, "text/") || f.Mime == "application/json":
		u += "?alt=media"
	default:
		return fmt.Sprintf("It isn't text (%s): it can't be read as words.", f.Mime), nil
	}
	var b bytes.Buffer
	if err := g.call(ctx, http.MethodGet, u, nil, "", &b); err != nil {
		return "", err
	}
	return b.String(), nil
}

func kindOf(mime string) string {
	switch mime {
	case googleDoc:
		return "Doc"
	case "application/vnd.google-apps.spreadsheet":
		return "Sheet"
	case "application/vnd.google-apps.presentation":
		return "Slides"
	}
	return "file"
}

func checkDoc(args map[string]string) error {
	if strings.TrimSpace(args["title"]) == "" || strings.TrimSpace(args["text"]) == "" {
		return errors.New("it needs a title and text")
	}
	return nil
}

func checkAdd(args map[string]string) error {
	if strings.TrimSpace(args["file"]) == "" || strings.TrimSpace(args["text"]) == "" {
		return errors.New("it needs the file and the text to add")
	}
	return nil
}

// folder is the "Tinio" folder agents make docs in, made the first time.
func (g *google) folder(ctx context.Context) (string, error) {
	if g.given.Access.Folder != "" {
		return g.given.Access.Folder, nil
	}
	raw, _ := json.Marshal(map[string]string{"name": "Tinio", "mimeType": folderMime})
	var f driveFile
	if err := g.call(ctx, http.MethodPost, driveAPI+"?fields=id", bytes.NewReader(raw), "application/json", &f); err != nil {
		return "", err
	}
	g.given.Access.Folder = f.ID
	if err := g.saver.SaveAccess(ctx, g.given.ID, g.given.Access); err != nil {
		return "", err
	}
	return f.ID, nil
}

func (g *google) makeDoc(ctx context.Context, args map[string]string) (string, error) {
	folder, err := g.folder(ctx)
	if err != nil {
		return "", err
	}
	meta := map[string]any{"name": cutRunes(args["title"], 200), "mimeType": googleDoc, "parents": []string{folder}}
	var f driveFile
	if err := g.upload(ctx, http.MethodPost, driveUpload+"?uploadType=multipart&fields=id,webViewLink", meta, args["text"], &f); err != nil {
		return "", err
	}
	return fmt.Sprintf("Made %q in the owner's Tinio folder: %s", args["title"], f.Link), nil
}

func (g *google) addToDoc(ctx context.Context, args map[string]string) (string, error) {
	f, ok, err := g.find(ctx, args["file"], true)
	if err != nil {
		return "", err
	}
	if !ok || f.Mime != googleDoc {
		return "", fmt.Errorf("%q isn't a doc you made in the owner's Tinio folder", args["file"])
	}
	before, err := g.text(ctx, f)
	if err != nil {
		return "", err
	}
	var out driveFile
	if err := g.upload(ctx, http.MethodPatch, driveUpload+"/"+url.PathEscape(f.ID)+"?uploadType=multipart&fields=id,webViewLink",
		map[string]any{}, strings.TrimRight(before, "\n")+"\n\n"+args["text"], &out); err != nil {
		return "", err
	}
	return fmt.Sprintf("Added to %q: %s", f.Name, out.Link), nil
}

// upload sends metadata and text as one multipart request; Drive makes the
// text a Google Doc.
func (g *google) upload(ctx context.Context, method, u string, meta map[string]any, text string, out any) error {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, _ := w.CreatePart(textproto.MIMEHeader{"Content-Type": {"application/json; charset=UTF-8"}})
	json.NewEncoder(part).Encode(meta)
	part, _ = w.CreatePart(textproto.MIMEHeader{"Content-Type": {"text/plain; charset=UTF-8"}})
	io.WriteString(part, text)
	w.Close()
	return g.call(ctx, method, u, &body, "multipart/related; boundary="+w.Boundary(), out)
}

// call makes a request as the owner and reads the answer into out (a
// *bytes.Buffer for raw text, else JSON).
func (g *google) call(ctx context.Context, method, u string, body io.Reader, contentType string, out any) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := g.client.Do(req)
	if err != nil {
		return fmt.Errorf("Google: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("Google answered %d: %s", resp.StatusCode, cutRunes(string(raw), 300))
	}
	if b, ok := out.(*bytes.Buffer); ok {
		b.Write(raw)
		return nil
	}
	return json.Unmarshal(raw, out)
}

func cutRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

func orEmpty(q string) string {
	if q = strings.TrimSpace(q); q == "" || q == "{}" {
		return "nothing"
	}
	return q
}

func orDefault(s, d string) string {
	if strings.TrimSpace(s) == "" {
		return d
	}
	return s
}
