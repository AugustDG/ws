package tmux

import "testing"

func TestParseLayout(t *testing.T) {
	// Three columns, each split into two rows.
	s := "3739,340x77,0,0{117x77,0,0[117x53,0,0,0,117x23,0,54,3],113x77,118,0[113x53,118,0,1,113x23,118,54,4],108x77,232,0[108x53,232,0,2,108x23,232,54,5]}"
	c, err := ParseLayout(s)
	if err != nil {
		t.Fatal(err)
	}
	if c.Split != CellColumns || len(c.Children) != 3 || c.W != 340 || c.H != 77 {
		t.Fatalf("root = %+v", c)
	}
	var ids []int
	for _, l := range c.Leaves() {
		ids = append(ids, l.PaneID)
	}
	want := []int{0, 3, 1, 4, 2, 5}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("leaf ids %v, want %v", ids, want)
		}
	}
	col := c.Children[1]
	if col.Split != CellRows || col.Children[0].Extent(CellRows) != 53 || col.Extent(CellColumns) != 113 {
		t.Errorf("middle column = %+v", col)
	}
}

func TestParseLayoutSinglePane(t *testing.T) {
	c, err := ParseLayout("b25d,80x24,0,0,7")
	if err != nil {
		t.Fatal(err)
	}
	if c.Split != CellLeaf || c.PaneID != 7 {
		t.Errorf("got %+v", c)
	}
}

func TestParseLayoutErrors(t *testing.T) {
	for _, s := range []string{"", "80x24,0,0,1", "abcd,80x24,0,0", "abcd,80x24,0,0{40x24,0,0,1", "abcd,80x24,0,0,1junk"} {
		if _, err := ParseLayout(s); err == nil {
			t.Errorf("ParseLayout(%q): expected an error", s)
		}
	}
}
