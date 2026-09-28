package tmux

import (
	"fmt"
	"strconv"
)

// Cell is one node of a parsed tmux layout string, as printed by
// #{window_layout}: "c3ab,200x50,0,0{100x50,0,0,1,99x50,101,0,2}".
// `{}` holds side-by-side children, `[]` holds stacked ones.
type Cell struct {
	W, H, X, Y int
	PaneID     int // -1 unless this is a leaf
	Split      CellSplit
	Children   []*Cell
}

type CellSplit int

const (
	CellLeaf CellSplit = iota
	CellColumns
	CellRows
)

// Leaves returns leaf cells in depth-first order, which is also the order
// select-layout assigns panes to cells.
func (c *Cell) Leaves() []*Cell {
	if c.Split == CellLeaf {
		return []*Cell{c}
	}
	var out []*Cell
	for _, ch := range c.Children {
		out = append(out, ch.Leaves()...)
	}
	return out
}

// Extent is the cell's size along the axis its parent splits on.
func (c *Cell) Extent(parent CellSplit) int {
	if parent == CellColumns {
		return c.W
	}
	return c.H
}

// ParseLayout parses a tmux layout string. The leading checksum is skipped.
func ParseLayout(s string) (*Cell, error) {
	p := &layoutParser{s: s}
	if len(s) < 5 || s[4] != ',' {
		return nil, fmt.Errorf("layout %q: missing checksum", s)
	}
	p.pos = 5
	cell, err := p.cell()
	if err != nil {
		return nil, fmt.Errorf("layout %q: %w", s, err)
	}
	if p.pos != len(s) {
		return nil, fmt.Errorf("layout %q: trailing data at %d", s, p.pos)
	}
	return cell, nil
}

type layoutParser struct {
	s   string
	pos int
}

func (p *layoutParser) cell() (*Cell, error) {
	c := &Cell{PaneID: -1}
	var err error
	if c.W, err = p.int('x'); err != nil {
		return nil, err
	}
	if c.H, err = p.int(','); err != nil {
		return nil, err
	}
	if c.X, err = p.int(','); err != nil {
		return nil, err
	}
	if c.Y, err = p.int(0); err != nil {
		return nil, err
	}

	switch p.peek() {
	case ',':
		// Leaves always end in ",<pane id>".
		p.pos++
		if c.PaneID, err = p.int(0); err != nil {
			return nil, err
		}
		return c, nil
	case '{', '[':
		open := p.s[p.pos]
		c.Split = CellColumns
		closer := byte('}')
		if open == '[' {
			c.Split, closer = CellRows, ']'
		}
		p.pos++
		for {
			child, err := p.cell()
			if err != nil {
				return nil, err
			}
			c.Children = append(c.Children, child)
			switch p.peek() {
			case ',':
				p.pos++
			case closer:
				p.pos++
				return c, nil
			default:
				return nil, fmt.Errorf("expected ',' or %q at %d", closer, p.pos)
			}
		}
	}
	return nil, fmt.Errorf("expected a pane id or a split at %d", p.pos)
}

func (p *layoutParser) peek() byte {
	if p.pos >= len(p.s) {
		return 0
	}
	return p.s[p.pos]
}

// int reads digits and, if sep is non-zero, consumes the separator after them.
func (p *layoutParser) int(sep byte) (int, error) {
	start := p.pos
	for p.pos < len(p.s) && p.s[p.pos] >= '0' && p.s[p.pos] <= '9' {
		p.pos++
	}
	if start == p.pos {
		return 0, fmt.Errorf("expected a number at %d", start)
	}
	n, _ := strconv.Atoi(p.s[start:p.pos])
	if sep != 0 {
		if p.peek() != sep {
			return 0, fmt.Errorf("expected %q at %d", sep, p.pos)
		}
		p.pos++
	}
	return n, nil
}
