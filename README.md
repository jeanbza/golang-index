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

### Usage with pkgsite worker

Point the pkgsite worker at this like so:

1. Make the worker accept a non-https index by commenting out the https validation at `pkgsite/internal/index/index.go`'s `New()` function.
1. Run the worker:

```sh
cd pkgsite
GO_MODULE_INDEX_URL=http://localhost:8081 go run cmd/worker/main.go -bypass_license_check=true
```

You may also be interested in periodically poking the worker to go do work with
something like:

```sh
# Assuming the worker is running at localhost:8000.
while true ; do date && curl localhost:8000/enqueue && curl localhost:8000/poll && sleep 20; done;
```

## Caveat emptor

There are lots of caveats:

- This fetches _every repo and tag_. It's only useful for github instances with a small/medium amount of content (~1000 repos with ~100 tags each, for example).
- This never updates its list of repos.
- This does not implement the full set of https://sum.golang.org/ -specified index features.
- pkgsite/worker expects the index to have an https endpoint. This only serves http.

Most of these are easy to fix, and may be fixed in the future.