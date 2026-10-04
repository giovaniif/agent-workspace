package main

import (
	"bytes"
	"slices"
)

var regexKeywords = []string{"return", "typeof", "case", "do", "else", "in", "of", "new", "delete", "void", "throw", "yield", "await", "instanceof"}

type scriptScan struct {
	name      string
	src       []byte
	js        bool
	i         int
	line      int
	findings  []finding
	prev      byte
	prevWord  string
	depth     int
	templates []int
}

func checkScript(name string, src []byte, js bool) []finding {
	s := &scriptScan{name: name, src: src, js: js, line: 1}
	s.code()
	return s.findings
}

func (s *scriptScan) report() {
	if n := len(s.findings); n == 0 || s.findings[n-1].line != s.line {
		s.findings = append(s.findings, finding{s.name, s.line})
	}
}

func (s *scriptScan) advance(n int) {
	for range n {
		if s.i >= len(s.src) {
			return
		}
		if s.src[s.i] == '\n' {
			s.line++
		}
		s.i++
	}
}

func (s *scriptScan) rest() []byte { return s.src[s.i:] }

func (s *scriptScan) code() {
	for s.i < len(s.src) {
		ch := s.src[s.i]
		switch {
		case ch == '\n' || ch == ' ' || ch == '\t' || ch == '\r':
			s.advance(1)
		case s.js && bytes.HasPrefix(s.rest(), []byte("//")):
			end := bytes.IndexByte(s.rest(), '\n')
			if end < 0 {
				end = len(s.rest())
			}
			if !scriptDirective.Match(s.rest()[:end]) {
				s.report()
			}
			s.advance(end)
		case bytes.HasPrefix(s.rest(), []byte("/*")):
			s.report()
			end := bytes.Index(s.rest()[2:], []byte("*/"))
			if end < 0 {
				s.advance(len(s.rest()))
			} else {
				s.advance(end + 4)
			}
			continue
		case ch == '\'' || ch == '"':
			s.quoted(ch)
			s.mark(ch)
		case s.js && ch == '`':
			s.advance(1)
			s.template()
		case s.js && ch == '/' && s.regexAllowed():
			s.regex()
			s.mark('a')
		case s.js && ch == '{':
			s.depth++
			s.advance(1)
			s.mark('{')
		case s.js && ch == '}':
			s.advance(1)
			if n := len(s.templates); n > 0 && s.templates[n-1] == s.depth-1 {
				s.templates = s.templates[:n-1]
				s.depth--
				s.template()
				continue
			}
			s.depth--
			s.mark('}')
		case isIdentByte(ch):
			start := s.i
			for s.i < len(s.src) && isIdentByte(s.src[s.i]) {
				s.i++
			}
			s.prev, s.prevWord = 'a', string(s.src[start:s.i])
		default:
			s.advance(1)
			s.mark(ch)
		}
	}
}

func (s *scriptScan) mark(ch byte) {
	s.prev, s.prevWord = ch, ""
}

func (s *scriptScan) regexAllowed() bool {
	if s.prev == 0 {
		return true
	}
	if s.prev == 'a' {
		return slices.Contains(regexKeywords, s.prevWord)
	}
	return bytes.IndexByte([]byte("(,=:[!&|?{};+-*%>~^"), s.prev) >= 0
}

func (s *scriptScan) quoted(q byte) {
	s.advance(1)
	for s.i < len(s.src) && s.src[s.i] != q && s.src[s.i] != '\n' {
		if s.src[s.i] == '\\' {
			s.advance(1)
		}
		s.advance(1)
	}
	if s.i < len(s.src) && s.src[s.i] == q {
		s.advance(1)
	}
}

func (s *scriptScan) regex() {
	s.advance(1)
	inClass := false
	for s.i < len(s.src) && s.src[s.i] != '\n' {
		switch s.src[s.i] {
		case '\\':
			s.advance(1)
		case '[':
			inClass = true
		case ']':
			inClass = false
		case '/':
			if !inClass {
				s.advance(1)
				return
			}
		}
		s.advance(1)
	}
}

func (s *scriptScan) template() {
	for s.i < len(s.src) {
		switch {
		case s.src[s.i] == '\\':
			s.advance(2)
		case s.src[s.i] == '`':
			s.advance(1)
			s.mark('a')
			return
		case bytes.HasPrefix(s.rest(), []byte("${")):
			s.templates = append(s.templates, s.depth)
			s.depth++
			s.advance(2)
			s.mark('{')
			return
		default:
			s.advance(1)
		}
	}
}

func isIdentByte(ch byte) bool {
	return ch == '_' || ch == '$' || ch >= '0' && ch <= '9' || ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= 0x80
}
