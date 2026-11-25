package folders

import (
	"archive/zip"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
)

// shop is a folder like the owner's, with things it must never show: a key,
// an .env file, a hidden folder, and a shortcut that leads out of it.
func shop(t *testing.T) (Folder, string) {
	t.Helper()
	top := t.TempDir()
	root := filepath.Join(top, "Leaf & Kettle")
	outside := filepath.Join(top, "Elsewhere")
	for _, d := range []string{root, outside, filepath.Join(root, "Menus"), filepath.Join(root, ".git")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		"Price list.txt":        "Shan Hills oolong: 14.20 a kilo",
		"Menus/Winter 2025.md":  "# Winter 2025\nJasmine, oolong",
		".env":                  "OPENAI_API_KEY=sk-secret",
		"id_rsa":                "PRIVATE KEY",
		"server.pem":            "CERT",
		".git/config":           "[core]",
		"../Elsewhere/diary.md": "private",
	}
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(outside, filepath.Join(root, "Out")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "diary.md"), filepath.Join(root, "diary.md")); err != nil {
		t.Fatal(err)
	}
	return Folder{ID: "f1", Name: "Leaf & Kettle", Path: root, Write: true}, outside
}

func TestOnlyWhatsInTheFolderMayBeNamed(t *testing.T) {
	f, _ := shop(t)
	r := Rules{}
	for _, ok := range []string{"", "Price list.txt", "Menus/Winter 2025.md", "Menus/New.md", "Menus/../Price list.txt"} {
		if _, err := r.Resolve(f, ok); err != nil {
			t.Errorf("%q refused: %v", ok, err)
		}
	}
	for _, bad := range []string{"../Elsewhere/diary.md", "/etc/passwd", ".env", "id_rsa", "server.pem", ".git/config", "Out/diary.md", "diary.md", "Nope/x.md"} {
		if p, err := r.Resolve(f, bad); err == nil {
			t.Errorf("%q let through as %s", bad, p)
		}
	}
	// "../" can't climb out: it's cleaned to a path inside.
	if p, err := r.Resolve(f, "../../Price list.txt"); err != nil || !strings.HasSuffix(p, "Leaf & Kettle/Price list.txt") {
		t.Errorf("climbing: %q, %v", p, err)
	}
}

func TestTinioOwnDataIsNeverRead(t *testing.T) {
	f, _ := shop(t)
	data := filepath.Join(f.Path, "Tinio data")
	os.MkdirAll(data, 0o700)
	os.WriteFile(filepath.Join(data, "key"), []byte("k"), 0o600)
	if _, err := (Rules{Data: data}).Resolve(f, "Tinio data/key"); !errors.Is(err, ErrOutside) {
		t.Errorf("Tinio's own key: %v", err)
	}
}

func TestWalkShowsOnlyWhatMayBeSeen(t *testing.T) {
	f, _ := shop(t)
	var seen []string
	skipped, more, err := Rules{}.Walk(f, 100, func(e Entry) bool { seen = append(seen, e.Rel); return true })
	if err != nil || more {
		t.Fatal(err, more)
	}
	got := strings.Join(seen, ",")
	if got != "Menus,Menus/Winter 2025.md,Price list.txt" {
		t.Errorf("seen %s", got)
	}
	if skipped != 6 { // .env, .git, id_rsa, server.pem, Out, diary.md
		t.Errorf("skipped %d", skipped)
	}
}

func TestTextOfWordAndSpreadsheets(t *testing.T) {
	dir := t.TempDir()
	doc := filepath.Join(dir, "menu.docx")
	zf, _ := os.Create(doc)
	z := zip.NewWriter(zf)
	w, _ := z.Create("word/document.xml")
	w.Write([]byte(`<w:document xmlns:w="w"><w:body><w:p><w:r><w:t>Winter</w:t></w:r></w:p><w:p><w:r><w:t>Oolong</w:t><w:tab/><w:t>14.20</w:t></w:r></w:p></w:body></w:document>`))
	z.Close()
	zf.Close()
	if text, err := Text(doc); err != nil || text != "Winter\nOolong\t14.20" {
		t.Errorf("docx: %q, %v", text, err)
	}
	x := excelize.NewFile()
	x.SetCellValue("Sheet1", "A1", "Tea")
	x.SetCellValue("Sheet1", "B1", 14.2)
	sheet := filepath.Join(dir, "prices.xlsx")
	if err := x.SaveAs(sheet); err != nil {
		t.Fatal(err)
	}
	if text, err := Text(sheet); err != nil || !strings.Contains(text, "Tea\t14.2") {
		t.Errorf("xlsx: %q, %v", text, err)
	}
	bin := filepath.Join(dir, "photo.jpg")
	os.WriteFile(bin, []byte{0xff, 0xd8, 0, 1, 2}, 0o644)
	if _, err := Text(bin); !errors.Is(err, ErrNotText) {
		t.Errorf("a picture: %v", err)
	}
}

func TestMakingAndChangingFiles(t *testing.T) {
	f, _ := shop(t)
	versions := t.TempDir()
	k := kit{s: Store{Rules: Rules{}, Versions: versions}, fs: []Folder{f}}

	mk := map[string]string{"folder": "leaf & kettle", "file": "Menus/Winter 2026.md", "text": "# Winter 2026"}
	if err := k.checkMake(mk); err != nil {
		t.Fatal(err)
	}
	if out, err := k.makeFile(mk, time.Now()); err != nil || !strings.Contains(out, "Made Menus/Winter 2026.md") {
		t.Fatal(out, err)
	}
	if err := k.checkMake(mk); err == nil {
		t.Error("made over a file that's there")
	}
	for _, bad := range []map[string]string{
		{"file": "Menus/x.docx", "text": "x"},
		{"file": "../Elsewhere/x.md", "text": "x"},
		{"file": ".env.md", "text": "x"},
		{"file": "Out/x.md", "text": "x"},
	} {
		if err := k.checkMake(bad); err == nil {
			t.Errorf("made %v", bad)
		}
	}

	change := map[string]string{"file": "Price list.txt", "find": "14.20", "replace": "14.80"}
	if err := k.checkChange(change); err != nil {
		t.Fatal(err)
	}
	if _, err := k.change(change, time.Date(2026, 11, 21, 9, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(f.Path, "Price list.txt")); string(b) != "Shan Hills oolong: 14.80 a kilo" {
		t.Errorf("changed to %q", b)
	}
	if b, err := os.ReadFile(filepath.Join(versions, "f1", "20261121-090000-Price list.txt")); err != nil || !strings.Contains(string(b), "14.20") {
		t.Errorf("old version: %q, %v", b, err)
	}
	if err := k.checkChange(map[string]string{"file": "Price list.txt", "find": "nowhere", "replace": "x"}); err == nil {
		t.Error("changed words that aren't there")
	}

	readOnly := kit{s: k.s, fs: []Folder{{ID: "f2", Name: "Receipts", Path: f.Path}}}
	if err := readOnly.checkMake(map[string]string{"file": "x.md", "text": "x"}); err == nil {
		t.Error("made a file in a read-only folder")
	}

	title, where, quote := k.explainChange(change)
	if title != "Change Price list.txt?" || where != "Leaf & Kettle" || quote != "− 14.20\n+ 14.80" {
		t.Errorf("explained %q %q %q", title, where, quote)
	}
	if _, where, _ := k.explainMake(mk); where != "Leaf & Kettle › Menus" {
		t.Errorf("where %q", where)
	}
}

// A git repository is changed by a coding job, when there are coding jobs:
// never directly, even one line. Without them, it's like any folder.
func TestARepositoryIsChangedByACodingJob(t *testing.T) {
	f, _ := shop(t) // it has a .git
	os.WriteFile(filepath.Join(f.Path, "README.md"), []byte("| skill | status |\n"), 0o644)
	change := map[string]string{"folder": "leaf & kettle", "file": "README.md", "find": "| skill | status |", "replace": "| skill | state |"}
	coding := kit{s: Store{Versions: t.TempDir()}, fs: []Folder{f}, coding: true}
	if err := coding.checkChange(change); err == nil || !strings.Contains(err.Error(), "coding job") {
		t.Errorf("changed a repository directly: %v", err)
	}
	if err := coding.checkMake(map[string]string{"folder": "leaf & kettle", "file": "NOTES.md", "text": "x"}); err == nil {
		t.Error("made a file in a repository directly")
	}
	plain := kit{s: Store{Versions: t.TempDir()}, fs: []Folder{f}}
	if err := plain.checkChange(change); err != nil {
		t.Errorf("without coding jobs, a repository is a folder like any: %v", err)
	}
}

func TestSearchAndRead(t *testing.T) {
	f, _ := shop(t)
	k := kit{s: Store{Rules: Rules{}}, fs: []Folder{f}}
	out, err := k.search(map[string]string{"query": "OOLONG"})
	if err != nil || !strings.Contains(out, "Price list.txt:\n  Shan Hills oolong: 14.20 a kilo") || !strings.Contains(out, "Menus/Winter 2025.md") {
		t.Errorf("search: %s, %v", out, err)
	}
	if out, _ := k.search(map[string]string{"query": "kilo shan"}); !strings.Contains(out, "14.20") {
		t.Errorf("words in another order: %s", out)
	}
	if out, _ := k.search(map[string]string{"query": "sk-secret"}); strings.Contains(out, ".env") {
		t.Error("searched an .env file")
	}
	if out, err := k.read(map[string]string{"file": "Menus/Winter 2025.md"}); err != nil || !strings.Contains(out, "Jasmine") {
		t.Errorf("read: %s, %v", out, err)
	}
	if out, err := k.list(map[string]string{}); err != nil || !strings.Contains(out, "Menus/") || strings.Contains(out, "id_rsa") || !strings.Contains(out, "left out") {
		t.Errorf("list: %s, %v", out, err)
	}
}
