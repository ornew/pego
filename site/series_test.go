package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// navConfig is the part of site/config.json that orders the navigation.
type navConfig struct {
	Sections []navConfigSection `json:"sections"`
}

type navConfigSection struct {
	Title    string             `json:"title"`
	Index    string             `json:"index"`
	First    []string           `json:"first"`
	Children []navConfigSection `json:"children"`
}

func readNavConfig(t *testing.T) map[string]navConfigSection {
	t.Helper()
	data, err := os.ReadFile("config.json")
	if err != nil {
		t.Fatal(err)
	}
	var c navConfig
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatal(err)
	}
	secs := map[string]navConfigSection{}
	for _, sec := range c.Sections {
		secs[sec.Title] = sec
	}
	return secs
}

var (
	indexGroupRe = regexp.MustCompile(`(?m)^#{2,3} (.+)$`)
	indexRowRe   = regexp.MustCompile(`(?m)^\| (?:(\d+) \| )?\[[^\]]+\]\(([^)#]+\.md)\) \|`)
)

// indexGroup is a heading of a section index and the pages its table lists.
type indexGroup struct {
	Title   string
	Pages   []string // repository paths
	Numbers []string // the numbers of the rows, when the table numbers them
}

// readIndexGroups returns the groups of a section index whose tables list pages of the section.
func readIndexGroups(t *testing.T, index string) []indexGroup {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", index))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	dir := filepath.ToSlash(filepath.Dir(index))
	// The text before the first heading is a group without a title.
	heads := append([][]int{{0, 0, 0, 0}}, indexGroupRe.FindAllStringSubmatchIndex(text, -1)...)
	var groups []indexGroup
	for i, h := range heads {
		end := len(text)
		if i+1 < len(heads) {
			end = heads[i+1][0]
		}
		g := indexGroup{Title: text[h[2]:h[3]]}
		for _, m := range indexRowRe.FindAllStringSubmatch(text[h[1]:end], -1) {
			if strings.Contains(m[2], "/") {
				continue
			}
			g.Pages = append(g.Pages, dir+"/"+m[2])
			g.Numbers = append(g.Numbers, m[1])
		}
		if len(g.Pages) > 0 {
			groups = append(groups, g)
		}
	}
	return groups
}

// TestIndexesMatchNavigation checks that the index of each grouped or numbered section lists its
// pages in the groups and the order of the navigation, and that numbered indexes number them as
// the navigation does.
func TestIndexesMatchNavigation(t *testing.T) {
	secs := readNavConfig(t)
	for _, name := range []string{"Cookbook", "Specification"} {
		sec := secs[name]
		groups := readIndexGroups(t, sec.Index)
		var got, want []string
		for _, g := range groups {
			got = append(got, g.Title+": "+strings.Join(g.Pages, " "))
		}
		for _, child := range sec.Children {
			want = append(want, child.Title+": "+strings.Join(child.First, " "))
		}
		if !slices.Equal(got, want) {
			t.Errorf("%s: the groups of %s are\n  %s\nbut the navigation has\n  %s", name, sec.Index, strings.Join(got, "\n  "), strings.Join(want, "\n  "))
		}
	}
	for _, name := range []string{"Tutorial", "Specification"} {
		sec := secs[name]
		var order []string
		for _, child := range sec.Children {
			order = append(order, child.First...)
		}
		order = append(order, sec.First...)
		order = slices.DeleteFunc(order, func(src string) bool { return numberedSections[name].Skip[src] })
		var listed, numbers []string
		for _, g := range readIndexGroups(t, sec.Index) {
			listed = append(listed, g.Pages...)
			numbers = append(numbers, g.Numbers...)
		}
		if !slices.Equal(listed, order) {
			t.Errorf("%s: %s lists %v, but the navigation numbers %v", name, sec.Index, listed, order)
		}
		for i, n := range numbers {
			if want := strconv.Itoa(i + 1); n != want {
				t.Errorf("%s: row %d of %s is numbered %q, want %s", name, i+1, sec.Index, n, want)
			}
		}
	}
}
