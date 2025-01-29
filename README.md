# golang-index

A (very) simple Golang index for https://sum.golang.org/. This is intended to be
used against an enterprise GitHub instance. You'll need two things:

- Your github instance hostname. ex `github.mycompany.net`
- A personal access token
  - Go to `github.mycompany.net`, click your profile, Settings, Developer Settings, Personal Access Tokens, Fine Grained Tokens
  - Generate a token with these settings:
    - Resource owner: your org
    - Repository access: all
    - Contents: read-only
    - Metadata: read-only

Then, run the app: `go run . -githubHostName=github.mycompany.net -githubAuthToken=github_pat_blahblah`.

## Motivation

[pkgsite](https://cs.opensource.google/go/x/pkgsite)'s [worker](https://cs.opensource.google/go/x/pkgsite/+/master:cmd/worker/) uses an index to figure out what to go scan for next. This index provides the minimal amount necessary for the worker to go do work.

### Why not seeddb?

[seeddb](https://cs.opensource.google/go/x/pkgsite/+/master:devtools/cmd/seeddb/) processes modules serially, which means that any time you're trying to query >100 modules it slows down quite a bit. It's easy to work around this problem, but I figured it'd be nicer to get the worker to do work, since it comes with a bunch of niceties that you'd want from seeddb eventually.

## Caveat emptor

There are lots of caveats:

- This fetches _every repo and tag_. It's only useful for github instances with a small/medium amount of content (~1000 repos with ~100 tags each, for example).
- This never updates its list of repos.
- This does not implement the full set of https://sum.golang.org/ -specified index features.
- pkgsite/worker expects the index to have an https endpoint. This only serves http. You can get around this issue by commenting out the https validation at `pkgsite/internal/index/index.go`'s `New()` function.

Most of these are easy to fix, and may be fixed in the future.