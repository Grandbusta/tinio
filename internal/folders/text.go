package folders

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/ledongthuc/pdf"
	"github.com/xuri/excelize/v2"
)

// maxFile is the largest file read: bigger ones are left alone.
const maxFile = 25 << 20

// ErrNotText is a file whose words can't be read: a picture, a program, an
// archive.
var ErrNotText = errors.New("not a file Tinio can read the words of")

// Text is the words in the file at path: plain text as it is, and the text of
// a Word document, a spreadsheet (a sheet at a time, cells tab-separated) or
// a PDF.
func Text(path string) (string, error) {
	st, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if st.IsDir() {
		return "", fmt.Errorf("it's a folder: list it instead")
	}
	if st.Size() > maxFile {
		return "", fmt.Errorf("it's %d MB, too big to read", st.Size()>>20)
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".docx":
		return docxText(path)
	case ".xlsx", ".xlsm":
		return xlsxText(path)
	case ".pdf":
		return pdfText(path)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if !utf8.Valid(b) || bytes.IndexByte(b, 0) >= 0 {
		return "", ErrNotText
	}
	return string(b), nil
}

// Writable is whether a file of this name is one an agent may make or
// change: plain text, whose words are all there is to it.
func Writable(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".md", ".markdown", ".txt", ".csv", ".tsv", ".json", ".yaml", ".yml", ".html", ".htm", ".ics", ".xml", ".rtf":
		return true
	}
	return false
}

func docxText(path string) (string, error) {
	z, err := zip.OpenReader(path)
	if err != nil {
		return "", ErrNotText
	}
	defer z.Close()
	for _, f := range z.File {
		if f.Name != "word/document.xml" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "", err
		}
		defer rc.Close()
		var out strings.Builder
		dec := xml.NewDecoder(io.LimitReader(rc, maxFile))
		inText := false
		for {
			tok, err := dec.Token()
			if err == io.EOF {
				break
			}
			if err != nil {
				return "", ErrNotText
			}
			switch t := tok.(type) {
			case xml.StartElement:
				switch t.Name.Local {
				case "t":
					inText = true
				case "tab":
					out.WriteByte('\t')
				case "br", "cr":
					out.WriteByte('\n')
				}
			case xml.EndElement:
				switch t.Name.Local {
				case "t":
					inText = false
				case "p":
					out.WriteByte('\n')
				}
			case xml.CharData:
				if inText {
					out.Write(t)
				}
			}
		}
		return strings.TrimSpace(out.String()), nil
	}
	return "", ErrNotText
}

func xlsxText(path string) (string, error) {
	x, err := excelize.OpenFile(path)
	if err != nil {
		return "", ErrNotText
	}
	defer x.Close()
	var out strings.Builder
	for _, sheet := range x.GetSheetList() {
		rows, err := x.GetRows(sheet)
		if err != nil {
			continue
		}
		fmt.Fprintf(&out, "Sheet %q\n", sheet)
		for _, row := range rows {
			out.WriteString(strings.Join(row, "\t"))
			out.WriteByte('\n')
		}
		out.WriteByte('\n')
	}
	return strings.TrimSpace(out.String()), nil
}

func pdfText(path string) (text string, err error) {
	defer func() { // a broken PDF can panic the reader
		if recover() != nil {
			text, err = "", ErrNotText
		}
	}()
	f, r, err := pdf.Open(path)
	if err != nil {
		return "", ErrNotText
	}
	defer f.Close()
	rd, err := r.GetPlainText()
	if err != nil {
		return "", ErrNotText
	}
	b, err := io.ReadAll(io.LimitReader(rd, maxFile))
	if err != nil {
		return "", ErrNotText
	}
	return strings.TrimSpace(string(b)), nil
}
