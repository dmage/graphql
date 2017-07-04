package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"os"

	"./fixmepkg"
	"golang.org/x/oauth2"
)

func main() {
	src := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: os.Getenv("GITHUB_TOKEN")},
	)
	httpClient := oauth2.NewClient(context.Background(), src)

	var q struct {
		Query     string                 `json:"query"`
		Variables map[string]interface{} `json:"variables,omitempty"`
	}
	q.Query = `
		query {
			repository(owner:"openshift",name:"origin") {
				description
				pullRequest(number:14521) {
					author {
						login
						... on User {
							company
						}
					}
				}
			}
		}
	`
	buf, err := json.Marshal(q)
	if err != nil {
		log.Fatal(err)
	}
	resp, err := httpClient.Post("https://api.github.com/graphql", "", bytes.NewReader(buf))
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()

	var r struct {
		Data struct {
			Repository fixmepkg.Repository
		}
	}
	err = json.NewDecoder(resp.Body).Decode(&r)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("%#+v", r)
	log.Println(*r.Data.Repository.Description())
	log.Printf("%#+v", r.Data.Repository.PullRequest().Author())
	log.Println(r.Data.Repository.PullRequest().Author().Login())
	//log.Println(*r.Data.Repository.PullRequest().Author().(fixmepkg.User).Company())
	//for _, item := range r.Data.Repository.PullRequest().Timeline().Nodes() {
	//	log.Println(item)
	//}
}
