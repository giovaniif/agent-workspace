package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var imageExt = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".mp4": true, ".webm": true, ".mov": true}

var githubSlug = regexp.MustCompile(`github\.com[:/]([^/]+/[^/]+?)(\.git)?$`)

const assetsBranch = "pr-assets"

func assets(e *env, args []string, r *result) {
	if len(args) == 0 {
		r.bad("assets needs push or check", "go run ./scripts/verify assets push --dir 42 shot.png")
		return
	}
	o, ok := parse(args[1:], []string{"repo", "remote", "slug", "dir", "base"}, nil, r)
	if !ok {
		return
	}
	repo, _ := filepath.Abs(o.get("repo", e.repo))
	switch args[0] {
	case "check":
		assetsCheck(e, repo, o.get("base", "origin/main"), r)
	case "push":
		assetsPush(e, repo, o, r)
	default:
		r.bad("unknown assets command "+args[0], "use push or check")
	}
}

func assetsCheck(e *env, repo, base string, r *result) {
	out, ok := e.git(repo, "diff", "--name-only", "--diff-filter=AMR", base+"...HEAD")
	if !ok {
		r.fail("cannot diff against "+base, "git fetch origin main")
		return
	}
	var imgs []string
	for _, f := range strings.Fields(out) {
		if imageExt[strings.ToLower(filepath.Ext(f))] && !strings.HasPrefix(f, "docs/images/readme/") && !strings.HasPrefix(f, "web/public/") && !strings.HasPrefix(f, "macos/") {
			imgs = append(imgs, f)
			r.fail("the PR branch commits an image: "+f, "remove it and push it with go run ./scripts/verify assets push --dir <pr> "+f)
		}
	}
	if imgs == nil {
		imgs = []string{}
	}
	r.set("images", imgs)
}

func assetsPush(e *env, repo string, o opts, r *result) {
	remote := o.get("remote", "origin")
	dir := strings.Trim(o.get("dir", ""), "/")
	if dir == "" || strings.Contains(dir, "..") {
		r.bad("assets push needs --dir <issue or PR number>", "--dir 42")
		return
	}
	if len(o.pos) == 0 {
		r.bad("assets push needs files", "go run ./scripts/verify assets push --dir 42 a.png")
		return
	}
	slug := o.get("slug", "")
	if slug == "" {
		url, _ := e.git(repo, "remote", "get-url", remote)
		if m := githubSlug.FindStringSubmatch(url); m != nil {
			slug = m[1]
		} else {
			r.bad("cannot read the GitHub repo from the remote "+remote, "--slug owner/repo")
			return
		}
	}
	idx := filepath.Join(e.state, "assets-index")
	_ = os.Remove(idx)
	defer func() { _ = os.Remove(idx) }()
	vars := append(os.Environ(), "GIT_INDEX_FILE="+idx)
	if email, _ := e.git(repo, "config", "user.email"); email == "" {
		vars = append(vars, "GIT_AUTHOR_NAME=crew-verify", "GIT_AUTHOR_EMAIL=crew-verify@users.noreply.github.com", "GIT_COMMITTER_NAME=crew-verify", "GIT_COMMITTER_EMAIL=crew-verify@users.noreply.github.com")
	}
	g := func(args ...string) (string, bool) {
		out, _, code := e.sys.exec(cmd{dir: repo, env: vars, name: "git", args: args})
		return strings.TrimSpace(out), code == 0
	}
	parent := ""
	if _, ok := g("fetch", "-q", remote, assetsBranch); ok {
		parent, _ = g("rev-parse", "FETCH_HEAD")
		if _, ok := g("read-tree", parent); !ok {
			r.fail("cannot read the "+assetsBranch+" tree", "git fetch "+remote+" "+assetsBranch)
			return
		}
	} else if _, ok := g("read-tree", "--empty"); !ok {
		r.fail("cannot make an empty index", "check the repo")
		return
	}
	var paths []string
	for _, f := range o.pos {
		if !imageExt[strings.ToLower(filepath.Ext(f))] {
			r.bad("not an image or video: "+f, "push only PNG, JPEG, GIF, WebP or MP4 files")
			return
		}
		sha, ok := g("hash-object", "-w", f)
		if !ok {
			r.fail("cannot read "+f, "give a file that exists")
			return
		}
		p := dir + "/" + filepath.Base(f)
		if _, ok := g("update-index", "--add", "--cacheinfo", "100644,"+sha+","+p); !ok {
			r.fail("cannot add "+p, "check the file name")
			return
		}
		paths = append(paths, p)
	}
	tree, ok := g("write-tree")
	if !ok {
		r.fail("cannot write the tree", "check the repo")
		return
	}
	ct := []string{"commit-tree", tree, "-m", "assets for " + dir}
	if parent != "" {
		ct = append(ct, "-p", parent)
	}
	commit, ok := g(ct...)
	if !ok {
		r.fail("cannot commit the assets", "set git user.name and user.email")
		return
	}
	if _, ok := g("push", "-q", remote, commit+":refs/heads/"+assetsBranch); !ok {
		r.fail("cannot push "+assetsBranch, "fetch "+assetsBranch+" and run the command again; another push may have won")
		return
	}
	var urls []string
	for _, p := range paths {
		urls = append(urls, "https://raw.githubusercontent.com/"+slug+"/"+commit+"/"+p)
	}
	r.set("commit", commit)
	r.set("urls", urls)
}
