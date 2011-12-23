// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package build

import (
	"bytes"
	"compress/gzip"
	"crypto/sha1"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"strings"

	"appengine"
	"appengine/datastore"
)

const maxDatastoreStringLen = 500

// A Package describes a package that is listed on the dashboard.
type Package struct {
	Name    string
	Path    string // (empty for the main Go tree)
	NextNum int    // Num of the next head Commit
}

func (p *Package) String() string {
	return fmt.Sprintf("%s: %q", p.Path, p.Name)
}

func (p *Package) Key(c appengine.Context) *datastore.Key {
	key := p.Path
	if key == "" {
		key = "go"
	}
	return datastore.NewKey(c, "Package", key, 0, nil)
}

// LastCommit returns the most recent Commit for this Package.
func (p *Package) LastCommit(c appengine.Context) (*Commit, os.Error) {
	var commits []*Commit
	_, err := datastore.NewQuery("Commit").
		Ancestor(p.Key(c)).
		Order("-Time").
		Limit(1).
		GetAll(c, &commits)
	if err != nil {
		return nil, err
	}
	if len(commits) != 1 {
		return nil, datastore.ErrNoSuchEntity
	}
	return commits[0], nil
}

// GetPackage fetches a Package by path from the datastore.
func GetPackage(c appengine.Context, path string) (*Package, os.Error) {
	p := &Package{Path: path}
	err := datastore.Get(c, p.Key(c), p)
	if err == datastore.ErrNoSuchEntity {
		return nil, fmt.Errorf("package %q not found", path)
	}
	return p, err
}

// A Commit describes an individual commit in a package.
//
// Each Commit entity is a descendant of its associated Package entity.
// In other words, all Commits with the same PackagePath belong to the same
// datastore entity group.
type Commit struct {
	PackagePath string // (empty for Go commits)
	Hash        string
	ParentHash  string
	Num         int // Internal monotonic counter unique to this package.

	User string
	Desc string `datastore:",noindex"`
	Time datastore.Time

	// ResultData is the Data string of each build Result for this Commit.
	// For non-Go commits, only the Results for the current Go tip, weekly,
	// and release Tags are stored here. This is purely de-normalized data.
	// The complete data set is stored in Result entities.
	ResultData []string `datastore:",noindex"`

	FailNotificationSent bool
}

func (com *Commit) Key(c appengine.Context) *datastore.Key {
	if com.Hash == "" {
		panic("tried Key on Commit with empty Hash")
	}
	p := Package{Path: com.PackagePath}
	key := com.PackagePath + "|" + com.Hash
	return datastore.NewKey(c, "Commit", key, 0, p.Key(c))
}

func (c *Commit) Valid() os.Error {
	if !validHash(c.Hash) {
		return os.NewError("invalid Hash")
	}
	if c.ParentHash != "" && !validHash(c.ParentHash) { // empty is OK
		return os.NewError("invalid ParentHash")
	}
	return nil
}

// AddResult adds the denormalized Reuslt data to the Commit's Result field.
// It must be called from inside a datastore transaction.
func (com *Commit) AddResult(c appengine.Context, r *Result) os.Error {
	if err := datastore.Get(c, com.Key(c), com); err != nil {
		return fmt.Errorf("getting Commit: %v", err)
	}
	com.ResultData = append(com.ResultData, r.Data())
	if _, err := datastore.Put(c, com.Key(c), com); err != nil {
		return fmt.Errorf("putting Commit: %v", err)
	}
	return nil
}

// Result returns the build Result for this Commit for the given builder/goHash.
func (c *Commit) Result(builder, goHash string) *Result {
	for _, r := range c.ResultData {
		p := strings.SplitN(r, "|", 4)
		if len(p) != 4 || p[0] != builder || p[3] != goHash {
			continue
		}
		return partsToHash(c, p)
	}
	return nil
}

// Results returns the build Results for this Commit for the given goHash.
func (c *Commit) Results(goHash string) (results []*Result) {
	for _, r := range c.ResultData {
		p := strings.SplitN(r, "|", 4)
		if len(p) != 4 || p[3] != goHash {
			continue
		}
		results = append(results, partsToHash(c, p))
	}
	return
}

// partsToHash converts a Commit and ResultData substrings to a Result.
func partsToHash(c *Commit, p []string) *Result {
	return &Result{
		Builder:     p[0],
		Hash:        c.Hash,
		PackagePath: c.PackagePath,
		GoHash:      p[3],
		OK:          p[1] == "true",
		LogHash:     p[2],
	}
}

// OK returns the Commit's build state for a specific builder and goHash.
func (c *Commit) OK(builder, goHash string) (ok, present bool) {
	r := c.Result(builder, goHash)
	if r == nil {
		return false, false
	}
	return r.OK, true
}

// A Result describes a build result for a Commit on an OS/architecture.
//
// Each Result entity is a descendant of its associated Commit entity.
type Result struct {
	Builder     string // "arch-os[-note]"
	Hash        string
	PackagePath string // (empty for Go commits)

	// The Go Commit this was built against (empty for Go commits).
	GoHash string

	OK      bool
	Log     string `datastore:"-"`        // for JSON unmarshaling only
	LogHash string `datastore:",noindex"` // Key to the Log record.

	RunTime int64 // time to build+test in nanoseconds 
}

func (r *Result) Key(c appengine.Context) *datastore.Key {
	p := Package{Path: r.PackagePath}
	key := r.Builder + "|" + r.PackagePath + "|" + r.Hash + "|" + r.GoHash
	return datastore.NewKey(c, "Result", key, 0, p.Key(c))
}

func (r *Result) Valid() os.Error {
	if !validHash(r.Hash) {
		return os.NewError("invalid Hash")
	}
	if r.PackagePath != "" && !validHash(r.GoHash) {
		return os.NewError("invalid GoHash")
	}
	return nil
}

// Data returns the Result in string format
// to be stored in Commit's ResultData field.
func (r *Result) Data() string {
	return fmt.Sprintf("%v|%v|%v|%v", r.Builder, r.OK, r.LogHash, r.GoHash)
}

// A Log is a gzip-compressed log file stored under the SHA1 hash of the
// uncompressed log text.
type Log struct {
	CompressedLog []byte
}

func (l *Log) Text() ([]byte, os.Error) {
	d, err := gzip.NewReader(bytes.NewBuffer(l.CompressedLog))
	if err != nil {
		return nil, fmt.Errorf("reading log data: %v", err)
	}
	b, err := ioutil.ReadAll(d)
	if err != nil {
		return nil, fmt.Errorf("reading log data: %v", err)
	}
	return b, nil
}

func PutLog(c appengine.Context, text string) (hash string, err os.Error) {
	h := sha1.New()
	io.WriteString(h, text)
	b := new(bytes.Buffer)
	z, _ := gzip.NewWriterLevel(b, gzip.BestCompression)
	io.WriteString(z, text)
	z.Close()
	hash = fmt.Sprintf("%x", h.Sum())
	key := datastore.NewKey(c, "Log", hash, 0, nil)
	_, err = datastore.Put(c, key, &Log{b.Bytes()})
	return
}

// A Tag is used to keep track of the most recent Go weekly and release tags.
// Typically there will be one Tag entity for each kind of hg tag.
type Tag struct {
	Kind string // "weekly", "release", or "tip"
	Name string // the tag itself (for example: "release.r60")
	Hash string
}

func (t *Tag) Key(c appengine.Context) *datastore.Key {
	p := &Package{}
	return datastore.NewKey(c, "Tag", t.Kind, 0, p.Key(c))
}

func (t *Tag) Valid() os.Error {
	if t.Kind != "weekly" && t.Kind != "release" && t.Kind != "tip" {
		return os.NewError("invalid Kind")
	}
	if !validHash(t.Hash) {
		return os.NewError("invalid Hash")
	}
	return nil
}

// GetTag fetches a Tag by name from the datastore.
func GetTag(c appengine.Context, tag string) (*Tag, os.Error) {
	t := &Tag{Kind: tag}
	if err := datastore.Get(c, t.Key(c), t); err != nil {
		if err == datastore.ErrNoSuchEntity {
			return nil, os.NewError("tag not found: " + tag)
		}
		return nil, err
	}
	if err := t.Valid(); err != nil {
		return nil, err
	}
	return t, nil
}

// Packages returns all non-Go packages.
func Packages(c appengine.Context) ([]*Package, os.Error) {
	var pkgs []*Package
	for t := datastore.NewQuery("Package").Run(c); ; {
		pkg := new(Package)
		if _, err := t.Next(pkg); err == datastore.Done {
			break
		} else if err != nil {
			return nil, err
		}
		if pkg.Path != "" {
			pkgs = append(pkgs, pkg)
		}
	}
	return pkgs, nil
}
