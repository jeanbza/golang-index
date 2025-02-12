package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/go-github/v68/github"
	"github.com/shurcooL/githubv4"
	"golang.org/x/oauth2"
	"golang.org/x/sync/errgroup"
)

var port = flag.Int("port", 8081, "port to listen on")
var githubHostName = flag.String("githubHostName", "", "github host to query. should be your enterprise host - ex: github.mycompany.net")
var githubAuthToken = flag.String("githubAuthToken", "", "github auth token")

const githubResultsPerPage = 100
const tagWorkers = 10

type JSONOut struct {
	Path      string `json:"Path"`
	Version   string `json:"Version"`
	Timestamp string `json:"Timestamp"`
}

func main() {
	flag.Parse()
	ctx := context.Background()

	if *githubHostName == "" || *githubAuthToken == "" {
		fmt.Println("--githubHostName and --githubAuthToken are required")
		os.Exit(1)
	}

	i, err := newIndex(ctx)
	if err != nil {
		fmt.Println(fmt.Errorf("error instantiating index: %v", err))
		os.Exit(1)
	}

	// TODO(jeanbza): This should re-run periodically.
	repoNames := make(chan string, 2*githubResultsPerPage)
	grp, grpCtx := errgroup.WithContext(ctx)
	grp.Go(func() error {
		return i.repos(grpCtx, repoNames)
	})
	for j := 0; j < tagWorkers; j++ {
		grp.Go(func() error {
			return i.tagsForRepos(grpCtx, repoNames)
		})
	}
	if err := grp.Wait(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		var lines []string

		i.mu.Lock()
		defer i.mu.Unlock()
		for repoName, tags := range i.repoTags {
			for _, tag := range tags {
				jo := JSONOut{Path: fmt.Sprintf("github.netflix.net/%s", repoName), Version: tag.tag, Timestamp: tag.commitDate.Format(time.RFC3339)}
				out, err := json.Marshal(&jo)
				if err != nil {
					panic(err)
				}
				lines = append(lines, string(out))
			}
		}
		if _, err := w.Write([]byte(strings.Join(lines, "\n"))); err != nil {
			panic(err)
		}
	})

	fmt.Printf("Server listening on :%d\n", port)
	// log.Fatal(http.ListenAndServeTLS(fmt.Sprintf(":%d", port), "host.cert", "host.key", nil))
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", port), nil))
}

type repoTag struct {
	tag        string
	commitDate time.Time
}

type index struct {
	// v3 API
	// TODO(jeanbza): Re-write all rest client calls with the graphql client
	// to simplify.
	restClient *github.Client
	// v4 API
	graphqlClient *githubv4.Client

	mu sync.Mutex
	// Map of repo name to tags.
	repoTags map[string][]*repoTag
}

func newIndex(ctx context.Context) (*index, error) {
	client := github.NewClient(nil)
	client, err := client.WithEnterpriseURLs(fmt.Sprintf("https://%s/api/v3/", *githubHostName), fmt.Sprintf("https://%s/api/uploads/", *githubHostName))
	if err != nil {
		return nil, fmt.Errorf("unable to start an enterprise client: %w", err)
	}
	client = client.WithAuthToken(*githubAuthToken)

	fullHost := fmt.Sprintf("https://%s/api/graphql", *githubHostName)
	src := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: *githubAuthToken},
	)
	httpClient := oauth2.NewClient(ctx, src)
	graphqlClient := githubv4.NewEnterpriseClient(fullHost, httpClient)

	return &index{
		restClient:    client,
		graphqlClient: graphqlClient,

		repoTags: make(map[string][]*repoTag),
	}, nil
}

// Get all the repos.
//
// Only one of this function should be run at a time.
func (i *index) repos(ctx context.Context, results chan<- string) error {
	defer close(results)

	// list public repositories for org "github"

	opt := &github.SearchOptions{
		ListOptions: github.ListOptions{PerPage: githubResultsPerPage},
	}
	// get all pages of results
	for {
		repos, resp, err := i.restClient.Search.Repositories(ctx, "language:golang", opt)
		if err != nil {
			return err
		}
		fmt.Printf("received %d repo results from github!\n", len(repos.Repositories))
		for _, r := range repos.Repositories {
			results <- *r.FullName
		}
		if resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}
	return nil
}

// Get all the tags for the repos.
//
// Multiple of this function can be run concurrently. Each invocation pulls a
// different repo from the queue and works on it independently.
func (i *index) tagsForRepos(ctx context.Context, repos <-chan string) error {
	for {
		repoName, more := <-repos
		fmt.Println("looking for tags for", repoName)
		if !more {
			fmt.Println("done looking for tags")
			break
		}
		tags, err := i.tagsForRepo(ctx, repoName)
		if err != nil {
			return fmt.Errorf("error getting tags for %s: %w", repoName, err)
		}
		fmt.Printf("got %d tags for %s\n", len(tags), repoName)

		// TODO(jeanbza): If we get a lot of lock contention, consider
		// batching this.
		i.mu.Lock()
		i.repoTags[repoName] = tags
		i.mu.Unlock()
	}
	return nil
}

func (i *index) tagsForRepo(ctx context.Context, repoName string) ([]*repoTag, error) {
	var q struct {
		Repository struct {
			Refs struct {
				Edges []struct {
					Node struct {
						Name   githubv4.String
						Target struct {
							Commit struct {
								AbbreviatedOid githubv4.String
								CommittedDate  githubv4.DateTime
							} `graphql:"... on Commit"`
						}
					}
				}
				PageInfo struct {
					EndCursor   githubv4.String
					HasNextPage bool
				}
			} `graphql:"refs(refPrefix: \"refs/tags/\", orderBy: {field: TAG_COMMIT_DATE, direction: DESC}, first: 100, after: $tagsCursor)"`
		} `graphql:"repository(owner: $repoOrg, name: $repoName)"`
	}
	parts := strings.Split(repoName, "/")
	if len(parts) != 2 {
		return nil, fmt.Errorf("expected org/name format, but got %d parts from %s", len(parts), repoName)
	}

	variables := map[string]interface{}{
		"repoOrg":    githubv4.String(parts[0]),
		"repoName":   githubv4.String(parts[1]),
		"tagsCursor": (*githubv4.String)(nil),
	}
	var tags []*repoTag
	// Page through all the results.
	for {
		if err := i.graphqlClient.Query(ctx, &q, variables); err != nil {
			return nil, fmt.Errorf("error querying tags for %s: %w", repoName, err)
		}
		for _, t := range q.Repository.Refs.Edges {
			tags = append(tags, &repoTag{tag: string(t.Node.Name), commitDate: t.Node.Target.Commit.CommittedDate.Time})
		}
		if !q.Repository.Refs.PageInfo.HasNextPage {
			break
		}
		variables["tagsCursor"] = githubv4.NewString(q.Repository.Refs.PageInfo.EndCursor)
	}

	return tags, nil
}
