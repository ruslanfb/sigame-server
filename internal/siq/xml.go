// Package siq implements the SIGame .siq package codec: import of format
// versions 3/4 (legacy <type>/<scenario> model) and 5 (<params>/<item> model)
// into the packs domain model, and export of version 5.
//
// See docs/research/02-siq-format.md for the format specification the codec
// follows and docs/packs.md for the user-facing compatibility matrix.
package siq

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
)

// Namespaces of content.xml. The reader is namespace-agnostic; the writer
// always emits NamespaceV5.
const (
	NamespaceV5 = "https://github.com/VladimirKhil/SI/blob/master/assets/siq_5.xsd"
	NamespaceV4 = "http://vladimirkhil.com/ygpackage3.0.xsd"
)

// utf8BOM is the byte-order mark every official content.xml starts with.
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// xmlDeclaration is written exactly as SIPackages does (lowercase utf-8, no newline).
const xmlDeclaration = `<?xml version="1.0" encoding="utf-8"?>`

// xmlPackage is the root <package> element. One struct set covers both v4 and
// v5: a question carries the deprecated <type>/<scenario> pair and the modern
// <params>/<script> pair side by side, and the mapper decides per question.
type xmlPackage struct {
	XMLName     xml.Name   `xml:"package"`
	Name        string     `xml:"name,attr"`
	Version     string     `xml:"version,attr,omitempty"`
	ID          string     `xml:"id,attr,omitempty"`
	Restriction string     `xml:"restriction,attr,omitempty"`
	Date        string     `xml:"date,attr,omitempty"`
	Publisher   string     `xml:"publisher,attr,omitempty"`
	ContactURI  string     `xml:"contactUri,attr,omitempty"`
	Difficulty  string     `xml:"difficulty,attr,omitempty"`
	Logo        string     `xml:"logo,attr,omitempty"`
	Language    string     `xml:"language,attr,omitempty"`
	Generator   string     `xml:"generator,attr,omitempty"`
	Xmlns       string     `xml:"xmlns,attr,omitempty"`
	Tags        []string   `xml:"tags>tag"`
	Global      *xmlGlobal `xml:"global"`
	Files       []xmlFile  `xml:"files>file"`
	Info        *xmlInfo   `xml:"info"`
	Rounds      []xmlRound `xml:"rounds>round"`
}

type xmlFile struct {
	Name string `xml:"name,attr"`
	Hash string `xml:"hash,attr"`
}

// xmlInfo is the <info> block present at every hierarchy level.
type xmlInfo struct {
	Authors         []string `xml:"authors>author"`
	Sources         []string `xml:"sources>source"`
	Comments        string   `xml:"comments,omitempty"`
	ShowmanComments string   `xml:"showmanComments,omitempty"`
	Extension       string   `xml:"extension,omitempty"`
}

// xmlGlobal mirrors v5 <global>; the same record bodies are used by v4
// Texts/authors.xml and Texts/sources.xml (see xmlAuthorsFile/xmlSourcesFile).
type xmlGlobal struct {
	Authors []xmlAuthor `xml:"Authors>Author"`
	Sources []xmlSource `xml:"Sources>Source"`
}

type xmlAuthor struct {
	ID         string `xml:"id,attr"`
	Name       string `xml:"Name"`
	SecondName string `xml:"SecondName"`
	Surname    string `xml:"Surname"`
	Country    string `xml:"Country"`
	City       string `xml:"City"`
}

type xmlSource struct {
	ID      string `xml:"id,attr"`
	Author  string `xml:"Author"`
	Title   string `xml:"Title"`
	Year    string `xml:"Year"`
	Publish string `xml:"Publish"`
	City    string `xml:"City"`
}

// xmlAuthorsFile is the root of v4 Texts/authors.xml.
type xmlAuthorsFile struct {
	XMLName xml.Name    `xml:"Authors"`
	Authors []xmlAuthor `xml:"Author"`
}

// xmlSourcesFile is the root of v4 Texts/sources.xml.
type xmlSourcesFile struct {
	XMLName xml.Name    `xml:"Sources"`
	Sources []xmlSource `xml:"Source"`
}

type xmlRound struct {
	Name   string     `xml:"name,attr"`
	Type   string     `xml:"type,attr,omitempty"`
	Info   *xmlInfo   `xml:"info"`
	Themes []xmlTheme `xml:"themes>theme"`
}

type xmlTheme struct {
	Name      string        `xml:"name,attr"`
	Info      *xmlInfo      `xml:"info"`
	Questions []xmlQuestion `xml:"questions>question"`
}

// xmlQuestion holds both the v4 (TypeElem, Scenario) and v5 (Type attribute,
// Params, Script) representations. encoding/xml keeps attributes and child
// elements in separate namespaces, so "type" may be used for both.
type xmlQuestion struct {
	Price    string       `xml:"price,attr"`
	Type     string       `xml:"type,attr,omitempty"`
	Info     *xmlInfo     `xml:"info"`
	TypeElem *xmlTypeElem `xml:"type"`
	Scenario *xmlScenario `xml:"scenario"`
	Params   *xmlParams   `xml:"params"`
	Script   *xmlScript   `xml:"script"`
	Right    *xmlAnswers  `xml:"right"`
	Wrong    *xmlAnswers  `xml:"wrong"`
}

type xmlAnswers struct {
	Answers []string `xml:"answer"`
}

// xmlTypeElem is the deprecated v4 <type name="..."><param name="...">v</param></type>.
type xmlTypeElem struct {
	Name   string           `xml:"name,attr"`
	Params []xmlSimpleParam `xml:"param"`
}

type xmlSimpleParam struct {
	Name  string `xml:"name,attr"`
	Value string `xml:",chardata"`
}

// xmlScenario is the deprecated v4 ordered atom list.
type xmlScenario struct {
	Atoms []xmlAtom `xml:"atom"`
}

type xmlAtom struct {
	Type string `xml:"type,attr,omitempty"`
	Time string `xml:"time,attr,omitempty"`
	Text string `xml:",chardata"`
}

type xmlParams struct {
	Params []xmlParam `xml:"param"`
}

// xmlParam is a v5 parameter. The XSD declares it as mixed content: a simple
// parameter carries its value as character data, a content parameter carries
// <item> children, a group carries nested <param> children and a numberSet
// carries one <numberSet/> child. Text is declared first so that the marshaller
// writes the value before any children.
type xmlParam struct {
	Name      string        `xml:"name,attr"`
	Type      string        `xml:"type,attr,omitempty"`
	IsRef     string        `xml:"isRef,attr,omitempty"`
	Text      string        `xml:",chardata"`
	Items     []xmlItem     `xml:"item"`
	Params    []xmlParam    `xml:"param"`
	NumberSet *xmlNumberSet `xml:"numberSet"`
}

type xmlNumberSet struct {
	Minimum string `xml:"minimum,attr"`
	Maximum string `xml:"maximum,attr"`
	Step    string `xml:"step,attr"`
}

// xmlItem is a v5 content item. durationMs is filled by the v4 upgrade path
// (atom time is a double in seconds) so that no precision is lost on the way.
type xmlItem struct {
	Type          string `xml:"type,attr,omitempty"`
	IsRef         string `xml:"isRef,attr,omitempty"`
	Placement     string `xml:"placement,attr,omitempty"`
	Duration      string `xml:"duration,attr,omitempty"`
	WaitForFinish string `xml:"waitForFinish,attr,omitempty"`
	Text          string `xml:",chardata"`

	durationMs int64
}

type xmlScript struct {
	Steps []xmlStep `xml:"step"`
}

type xmlStep struct {
	Type   string     `xml:"type,attr,omitempty"`
	Params []xmlParam `xml:"param"`
}

// nsAgnostic is an xml.TokenReader that erases every namespace from element
// and attribute names and drops xmlns declarations. Feeding it to
// xml.NewTokenDecoder yields a decoder that matches struct tags by local name
// only, whatever namespace (v4, v5, none, or a prefix) the document uses.
// Dropping the xmlns attributes is essential: the outer decoder would
// otherwise re-apply the default namespace it sees in them.
type nsAgnostic struct {
	inner xml.TokenReader
}

func (n nsAgnostic) Token() (xml.Token, error) {
	tok, err := n.inner.Token()
	switch t := tok.(type) {
	case xml.StartElement:
		t.Name.Space = ""
		attrs := t.Attr[:0]
		for _, a := range t.Attr {
			if a.Name.Space == "xmlns" || (a.Name.Space == "" && a.Name.Local == "xmlns") {
				continue
			}
			a.Name.Space = ""
			attrs = append(attrs, a)
		}
		t.Attr = attrs
		return t, err
	case xml.EndElement:
		t.Name.Space = ""
		return t, err
	}
	return tok, err
}

// newDecoder returns a namespace-agnostic decoder over data with the UTF-8
// BOM removed.
func newDecoder(data []byte) *xml.Decoder {
	data = bytes.TrimPrefix(data, utf8BOM)
	inner := xml.NewDecoder(bytes.NewReader(data))
	return xml.NewTokenDecoder(nsAgnostic{inner: inner})
}

// decodeContentXML parses content.xml of any supported version.
func decodeContentXML(data []byte) (*xmlPackage, error) {
	var p xmlPackage
	if err := newDecoder(data).Decode(&p); err != nil {
		return nil, fmt.Errorf("%w: content.xml: %v", ErrBadXML, err)
	}
	return &p, nil
}

// decodeAuthorsFile parses v4 Texts/authors.xml. An empty stub yields no records.
func decodeAuthorsFile(data []byte) ([]xmlAuthor, error) {
	var f xmlAuthorsFile
	if err := newDecoder(data).Decode(&f); err != nil {
		return nil, fmt.Errorf("%w: Texts/authors.xml: %v", ErrBadXML, err)
	}
	return f.Authors, nil
}

// decodeSourcesFile parses v4 Texts/sources.xml.
func decodeSourcesFile(data []byte) ([]xmlSource, error) {
	var f xmlSourcesFile
	if err := newDecoder(data).Decode(&f); err != nil {
		return nil, fmt.Errorf("%w: Texts/sources.xml: %v", ErrBadXML, err)
	}
	return f.Sources, nil
}

// encodeContentXML serialises a v5 package exactly the way SIPackages does:
// UTF-8 BOM, lowercase declaration, no indentation, xmlns on the root.
func encodeContentXML(w io.Writer, p *xmlPackage) error {
	p.Xmlns = NamespaceV5
	if _, err := w.Write(utf8BOM); err != nil {
		return err
	}
	if _, err := io.WriteString(w, xmlDeclaration); err != nil {
		return err
	}
	enc := xml.NewEncoder(w)
	if err := enc.Encode(p); err != nil {
		return fmt.Errorf("encode content.xml: %w", err)
	}
	return enc.Close()
}
