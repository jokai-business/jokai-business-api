package imports

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
)

var ErrWorkbook = errors.New("unsupported workbook")
var headers = map[string][]string{"customer": {"name", "email", "phone", "website", "industry"}, "opportunity": {"customer", "title", "value", "currency", "stage", "owner"}}

const MaxFile = 2 << 20
const MaxRows = 500

// Standard-template XLSX reader. No formulas, external workbook links, macros, hidden
// rows/columns, merged cells, or extra sheets are accepted or evaluated.
func Parse(data []byte, kind string) ([][]string, error) {
	head, ok := headers[kind]
	if !ok || len(data) > MaxFile {
		return nil, ErrWorkbook
	}
	z, e := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if e != nil || len(z.File) > 100 {
		return nil, ErrWorkbook
	}
	files := map[string][]byte{}
	total := 0
	for _, f := range z.File {
		if f.FileInfo().IsDir() {
			continue
		}
		if _, ok := files[f.Name]; ok || path.Clean(f.Name) != f.Name || strings.HasPrefix(f.Name, "/") || strings.Contains(f.Name, "..") || strings.Contains(strings.ToLower(f.Name), "vbaproject") || strings.Contains(f.Name, "externalLinks/") {
			return nil, ErrWorkbook
		}
		if f.UncompressedSize64 > 8<<20 {
			return nil, ErrWorkbook
		}
		r, e := f.Open()
		if e != nil {
			return nil, ErrWorkbook
		}
		b, e := io.ReadAll(io.LimitReader(r, 8<<20+1))
		r.Close()
		total += len(b)
		if e != nil || total > 8<<20 {
			return nil, ErrWorkbook
		}
		files[f.Name] = b
	}
	var wb struct {
		Sheets []struct {
			Name  string `xml:"name,attr"`
			ID    string `xml:"id,attr"`
			State string `xml:"state,attr"`
		} `xml:"sheets>sheet"`
	}
	if xml.Unmarshal(files["xl/workbook.xml"], &wb) != nil || len(wb.Sheets) != 1 || wb.Sheets[0].State != "" && wb.Sheets[0].State != "visible" {
		return nil, ErrWorkbook
	}
	var rel struct {
		Items []struct {
			ID     string `xml:"Id,attr"`
			Target string `xml:"Target,attr"`
			Mode   string `xml:"TargetMode,attr"`
			Type   string `xml:"Type,attr"`
		} `xml:"Relationship"`
	}
	if xml.Unmarshal(files["xl/_rels/workbook.xml.rels"], &rel) != nil {
		return nil, ErrWorkbook
	}
	sheetPath := ""
	for _, v := range rel.Items {
		if v.Mode == "External" {
			return nil, ErrWorkbook
		}
		if v.ID == wb.Sheets[0].ID && strings.HasSuffix(v.Type, "/worksheet") {
			if strings.HasPrefix(v.Target, "/") {
				sheetPath = strings.TrimPrefix(v.Target, "/")
			} else {
				sheetPath = path.Clean("xl/" + v.Target)
			}
		}
	}
	if !strings.HasPrefix(sheetPath, "xl/worksheets/") {
		return nil, ErrWorkbook
	}
	type rich struct {
		Text string `xml:"t"`
		Runs []struct {
			Text string `xml:"t"`
		} `xml:"r"`
	}
	readRich := func(r rich) string {
		v := r.Text
		for _, s := range r.Runs {
			v += s.Text
		}
		return v
	}
	shared := []string{}
	if b, ok := files["xl/sharedStrings.xml"]; ok {
		var ss struct {
			Items []rich `xml:"si"`
		}
		if xml.Unmarshal(b, &ss) != nil || len(ss.Items) > 5000 {
			return nil, ErrWorkbook
		}
		for _, r := range ss.Items {
			shared = append(shared, readRich(r))
		}
	}
	var sheet struct {
		Rows []struct {
			Number int  `xml:"r,attr"`
			Hidden bool `xml:"hidden,attr"`
			Cells  []struct {
				Ref     string  `xml:"r,attr"`
				Type    string  `xml:"t,attr"`
				Formula *string `xml:"f"`
				Value   string  `xml:"v"`
				Inline  rich    `xml:"is"`
			} `xml:"c"`
		} `xml:"sheetData>row"`
		Merges  []struct{} `xml:"mergeCells>mergeCell"`
		Columns []struct {
			Hidden bool `xml:"hidden,attr"`
		} `xml:"cols>col"`
	}
	if xml.Unmarshal(files[sheetPath], &sheet) != nil || len(sheet.Rows) < 2 || len(sheet.Rows) > MaxRows+1 || len(sheet.Merges) > 0 {
		return nil, ErrWorkbook
	}
	for _, c := range sheet.Columns {
		if c.Hidden {
			return nil, ErrWorkbook
		}
	}
	out := [][]string{}
	previous := 0
	for _, row := range sheet.Rows {
		if row.Hidden || row.Number <= previous || row.Number > MaxRows+1 {
			return nil, ErrWorkbook
		}
		previous = row.Number
		values := make([]string, len(head))
		seen := map[int]bool{}
		for _, c := range row.Cells {
			if c.Formula != nil {
				return nil, ErrWorkbook
			}
			i := 0
			for i < len(c.Ref) && c.Ref[i] >= 'A' && c.Ref[i] <= 'Z' {
				i++
			}
			if i == 0 {
				return nil, ErrWorkbook
			}
			col := 0
			for _, v := range c.Ref[:i] {
				col = col*26 + int(v-'A'+1)
			}
			n, e := strconv.Atoi(c.Ref[i:])
			if e != nil || n != row.Number || col < 1 || col > len(head) || seen[col] {
				return nil, ErrWorkbook
			}
			seen[col] = true
			value := c.Value
			switch c.Type {
			case "inlineStr":
				value = readRich(c.Inline)
			case "s":
				n, e := strconv.Atoi(value)
				if e != nil || n < 0 || n >= len(shared) {
					return nil, ErrWorkbook
				}
				value = shared[n]
			case "", "n", "str":
			case "b", "e", "d":
				return nil, ErrWorkbook
			default:
				return nil, ErrWorkbook
			}
			if len(value) > 2048 {
				return nil, ErrWorkbook
			}
			values[col-1] = strings.TrimSpace(value)
		}
		if row.Number == 1 {
			for i, h := range head {
				if values[i] != h {
					return nil, ErrWorkbook
				}
			}
			continue
		}
		// Require contiguous rows so error row numbers match the actual worksheet.
		if row.Number != len(out)+2 {
			return nil, ErrWorkbook
		}
		out = append(out, values)
	}
	if len(out) == 0 || sheet.Rows[0].Number != 1 {
		return nil, ErrWorkbook
	}
	return out, nil
}
func Template(kind string) ([]byte, error) {
	head, ok := headers[kind]
	if !ok {
		return nil, ErrWorkbook
	}
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	files := map[string]string{
		"[Content_Types].xml":        `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/></Types>`,
		"_rels/.rels":                `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`,
		"xl/workbook.xml":            `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Import" sheetId="1" r:id="rId1"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/></Relationships>`,
	}
	sheet := `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><cols><col min="1" max="6" width="25" customWidth="1" style="0"/></cols><sheetData><row r="1">`
	for i, h := range head {
		sheet += fmt.Sprintf(`<c r="%c1" t="inlineStr"><is><t>%s</t></is></c>`, 'A'+i, h)
	}
	files["xl/worksheets/sheet1.xml"] = sheet + `</row></sheetData></worksheet>`
	for name, value := range files {
		w, e := z.Create(name)
		if e != nil {
			return nil, e
		}
		if _, e = w.Write([]byte(value)); e != nil {
			return nil, e
		}
	}
	if e := z.Close(); e != nil {
		return nil, e
	}
	return b.Bytes(), nil
}
