package mind

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Grandbusta/tinio/internal/memory"
	"github.com/Grandbusta/tinio/internal/perception"
	"github.com/Grandbusta/tinio/internal/providers/system2"
	"github.com/Grandbusta/tinio/internal/render"
)

type sentFile struct {
	name, caption string
	data          []byte
	work          string
}

type fakeFiler struct{ sent []sentFile }

func (f *fakeFiler) SendFile(_ context.Context, _ perception.Percept, file render.File, caption string) error {
	f.sent = append(f.sent, sentFile{file.Name, caption, file.Data, file.Work})
	return nil
}

func TestPromisesCanBeSentAsAPDF(t *testing.T) {
	s2 := &fakeSystem2{promises: []system2.Promise{
		{By: "2026-10-27T20:00", What: "Tobi writes Sam a plan for the week", As: "pdf"},
		{By: "2026-10-27T21:00", What: "Tobi records Sam a song", As: "mp3"},
	}}
	m, s := promiseMind(&fakeReplier{}, s2)
	m.handle(context.Background(), percept("1", "Can you send me a plan as a PDF?"))
	if len(s.intentions) != 2 || s.intentions[0].Args["as"] != "" || s.intentions[1].Args["as"] != "" {
		t.Errorf("without a way to send files: %+v", s.intentions)
	}

	m, s = promiseMind(&fakeReplier{}, s2)
	m.Filer = &fakeFiler{}
	m.handle(context.Background(), percept("1", "Can you send me a plan as a PDF?"))
	if len(s.intentions) != 2 || s.intentions[0].Args["as"] != "pdf" || s.intentions[1].Args["as"] != "" {
		t.Errorf("intentions = %+v", s.intentions)
	}
}

// Asked to send something already promised as a different file, a move
// changes what it goes as, if it can send that; one it can't stays as it was.
func TestAMoveCanChangeWhatAPromiseGoesAs(t *testing.T) {
	r, filer := &fakeReplier{}, &fakeFiler{}
	s2 := &fakeSystem2{formats: map[string]string{"activity": weekJSON}}
	m, s := promiseMind(r, s2)
	m.Filer = filer
	plan := promised("Tobi writes Sam a plan for the week", "", "", now.Add(20*time.Hour))
	song := promised("Tobi writes Sam a song", "", "", now.Add(30*time.Hour))
	s.intentions = []memory.Intention{plan, song}

	s2.moves = []system2.Move{{Promise: "p1", By: "now", As: "pdf"}, {Promise: "p2", By: "2026-10-29T10:00", As: "mp3"}}
	m.handle(context.Background(), percept("1", "Send the plan now, as a PDF? And the song as an mp3"))
	if got := s.intention(plan.ID).Args["as"]; got != "pdf" {
		t.Fatalf("the plan goes as %q, want pdf", got)
	}
	if got := s.intention(song.ID).Args["as"]; got != "" {
		t.Fatalf("the song goes as %q, a file it can't send", got)
	}

	m.tick(context.Background(), now.Add(time.Minute))
	if len(filer.sent) != 1 || filer.sent[0].name != "sam-s-week.pdf" {
		t.Fatalf("files sent: %+v", filer.sent)
	}
}

const weekJSON = `{"title":"Sam's Week","kind":"writing","text":"Sam's Week\n\n## Monday\n\n- Call the clinic","project_notes":"",
	"project_done":false,"remember":[]}`

func TestAPDFPromiseIsSentAsAFile(t *testing.T) {
	r, filer := &fakeReplier{}, &fakeFiler{}
	s2 := &fakeSystem2{formats: map[string]string{"activity": weekJSON}}
	m, s := promiseMind(r, s2)
	m.Filer = filer
	i := promised("Tobi writes Sam a plan for the week", "", "", now.Add(-time.Minute))
	i.Args = map[string]string{"as": "pdf"}
	s.intentions = []memory.Intention{i}
	m.tick(context.Background(), now)

	if len(r.sent) != 0 || len(filer.sent) != 1 {
		t.Fatalf("messages %q, files %d", r.sent, len(filer.sent))
	}
	if f := filer.sent[0]; f.name != "sam-s-week.pdf" || f.caption != "a thoughtful reply" || !bytes.HasPrefix(f.data, []byte("%PDF-")) {
		t.Errorf("file %s, caption %q, %d bytes", f.name, f.caption, len(f.data))
	}
	if got := s.intention(i.ID); got.Status != memory.IntentionDone {
		t.Errorf("intention = %+v", got)
	}
}

func TestAnApprovedPDFGoesWithItsFile(t *testing.T) {
	r, filer := &fakeReplier{}, &fakeFiler{}
	s2 := &fakeSystem2{formats: map[string]string{"activity": weekJSON}}
	m, s, asker := gatedMind(r, s2)
	m.Filer = filer
	i := promised("Tobi writes Sam a plan for the week", "", "", now.Add(-time.Minute))
	i.Args = map[string]string{"as": "pdf"}
	s.intentions = []memory.Intention{i}
	m.tick(context.Background(), now)

	if len(asker.asked) != 1 || !strings.Contains(asker.asked[0], "📎 sam-s-week.pdf") || len(filer.sent) != 0 {
		t.Fatalf("asked %q, files %d", asker.asked, len(filer.sent))
	}
	m.handle(context.Background(), tap(owner, asker.ids[0], true))
	if len(filer.sent) != 1 || filer.sent[0].caption != "a thoughtful reply" || len(r.sent) != 0 {
		t.Fatalf("files %d, messages %q", len(filer.sent), r.sent)
	}
}
