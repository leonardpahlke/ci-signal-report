/*
Copyright 2021 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package cireporter

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

// GithubReport used to implement RequestData & Print for github report data
type GithubReport struct {
	ReportData ReportData
}

// RequestData this function is used to get github report data
func (r *GithubReport) RequestData(meta Meta, wg *sync.WaitGroup) ReportData {
	// labels=kind/failing-test&since=2021-09-01&sort=updated&per_page=100&page=1
	requestCfg := []GithubIssueRequest{
		{
			Owner:     "kubernetes",
			Repo:      "kubernetes",
			Params:    GithubIssueRequestParameters{IssueReqParamLabels: "kind/failing-test", IssueReqParamSince: "2021-09-01", IssueReqParamSort: "updated", IssueReqParamPerpage: "20"},
			AuthToken: meta.Env.GithubToken,
		},
		// {
		// 	Owner:     "kubernetes",
		// 	Repo:      "kubernetes",
		// 	Params:    GithubIssueRequestParameters{IssueReqParamLabels: "kind/flake", IssueReqParamSince: "2021-09-01", IssueReqParamSort: "updated", IssueReqParamPerpage: "20"},
		// 	AuthToken: meta.Env.GithubToken,
		// },
	}
	// request github issue data
	allReqGithubIssues := GithubIssuesAfterId{}
	var internalWg sync.WaitGroup
	for _, cfg := range requestCfg {
		internalWg.Add(1)
		go func(cfg GithubIssueRequest) {
			githubIssues := GetGithubIssues(cfg)
			for k, v := range githubIssues {
				allReqGithubIssues[k] = v
			}
			internalWg.Done()
		}(cfg)
	}
	internalWg.Wait()
	// DataPostProcessing collects data requested via assembleGithubRequests/2 and returns ReportData
	return meta.DataPostProcessing(r, githubReport, transformIntoReportData(meta, allReqGithubIssues), wg)
}

// Print extends GithubReport and prints report data to the console
func (r GithubReport) Print(meta Meta, reportData ReportData) {
	// Print regular out
	for _, data := range reportData.Data {
		// print sorted report data
		for _, records := range data.Records {
			fmt.Printf("#%d %s %s\n", records.ID, records.Highlight, records.Sig)
			fmt.Printf("- %s\n", records.Title)
			fmt.Printf("- %s\n", records.URL)
			for _, note := range records.Notes {
				fmt.Printf("- %s\n", note)
			}
		}
	}
}

// PutData extends GithubReport and stores the data at runtime to the struct val ReportData
func (r *GithubReport) PutData(reportData ReportData) {
	r.ReportData = reportData
}

// GetData extends GithubReport and returns the data that has been stored at runtime int the struct val ReportData (counter to SaveData/1)
func (r GithubReport) GetData() ReportData {
	return r.ReportData
}

// run all github requests to assemble data
func transformIntoReportData(meta Meta, issues GithubIssuesAfterId) chan ReportDataField {
	c := make(chan ReportDataField)
	go func() {
		defer close(c)
		var wg sync.WaitGroup
		for _, issue := range issues {
			wg.Add(1)
			go func(issue GithubIssueElement) {
				sigRegex := regexp.MustCompile(`sig/[a-zA-Z]+`)
				sigsInvolved := []string{}
				notes := []string{fmt.Sprintf("Created %s, Updated %s, Comments: %d", strings.Split(issue.CreatedAt, "T")[0], strings.Split(issue.UpdatedAt, "T")[0], issue.Comments)}
				lablesToNote := ""
				for _, label := range issue.Labels {
					sig := sigRegex.FindString(label.Name)
					if sig != "" {
						sigsInvolved = append(sigsInvolved, sig)
					}
					if strings.Contains(label.Name, "priority") {
						lablesToNote += fmt.Sprintf("%s%s%s ", colorGreen, label.Name, colorReset)
					}
					if strings.Contains(label.Name, "kind/") {
						lablesToNote += fmt.Sprintf("%s%s%s ", colorRed, label.Name, colorReset)
					}
				}
				if issue.Milestone != nil {
					lablesToNote += fmt.Sprintf("%smilestone %s%s", colorBlue, issue.Milestone.Title, colorReset)
				}
				if lablesToNote != "" {
					notes = append(notes, lablesToNote)
				}
				highlight := ""
				if checkTimeBefore(issue.CreatedAt, time.Now().AddDate(0, -3, 0)) {
					highlight += statusOldEmoji
				}
				if !checkTimeBefore(issue.CreatedAt, time.Now().AddDate(0, 0, -5)) {
					highlight += statusNewEmoji
				}
				c <- ReportDataField{
					Emoji: "",
					Title: "",
					Records: []ReportDataRecord{
						{
							URL:       issue.HTMLURL,
							ID:        issue.Number,
							Title:     issue.Title,
							Notes:     notes,
							Sig:       fmt.Sprintf("%v", sigsInvolved),
							Highlight: highlight,
						},
					},
				}
				wg.Done()
			}(issue)
		}
		wg.Wait()
	}()
	return c
}

func GetGithubIssues(cfg GithubIssueRequest) GithubIssuesAfterId {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/issues%s", cfg.Owner, cfg.Repo, "?state=open")
	for param, val := range cfg.Params {
		url += fmt.Sprintf("&%s=%s", param, val)
	}
	collectedIssues := GithubIssuesAfterId{}
	for issues := range assembleGithubIssues(url, cfg.AuthToken) {
		for k, issue := range issues {
			collectedIssues[k] = issue
		}
	}
	return collectedIssues
}

func assembleGithubIssues(url string, authToken string) chan GithubIssuesAfterId {
	c := make(chan GithubIssuesAfterId)
	go func() {
		defer close(c)
		wg := sync.WaitGroup{}
		wg.Add(1)
		go requestGithubIssues(c, &wg, url, 1, authToken)
		wg.Wait()
	}()
	return c
}

// requestGithubIssues sends a http request to github to list issues
func requestGithubIssues(c chan GithubIssuesAfterId, wg *sync.WaitGroup, url string, page int, authToken string) {
	url = fmt.Sprintf("%s&%s=%d", url, string(IssueReqParamPage), page)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Fatalf("Error on creating http request.\n[ERROR] -%v", err)
	}
	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", authToken))
	// Send http request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Fatalf("Error on sending http request.\n[ERROR] -%v", err)
	}
	defer resp.Body.Close()
	// Read body and unmarshal bytes
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Fatalf("Error on read from response body.\n[ERROR] -%v", err)
	}
	requestedIssues, err := UnmarshalGithubIssue(body)
	if err != nil {
		fmt.Println(url)
		fmt.Println(string(body))
		log.Fatalf("Error on UnmarshalGithubIssue.\n[ERROR] -%v", err)
	}
	// if result is not empty, request data from next website too
	if len(requestedIssues) != 0 {
		page++
		wg.Add(1)
		go requestGithubIssues(c, wg, url, page, authToken)
	}
	c <- filterGithubIssues(requestedIssues)
	wg.Done()
}

func filterGithubIssues(issues GithubIssues) GithubIssuesAfterId {
	filteredIssues := GithubIssuesAfterId{}
	for _, i := range issues {
		fine := true
		for _, label := range i.Labels {
			// issues should not contain any of these lables
			fine = fine && !strings.Contains(label.Name, "priority/backlog")
			fine = fine && !strings.Contains(label.Name, "triage/accepted")
			fine = fine && !strings.Contains(label.Name, "lifecycle/rotten")
			fine = fine && !strings.Contains(label.Name, "lifecycle/stale")
		}
		// issues should not be a pull request
		fine = fine && !strings.Contains(i.HTMLURL, "pull")
		if fine {
			filteredIssues[i.Number] = i
		}
	}
	return filteredIssues
}

func checkTimeBefore(s string, u time.Time) bool {
	layout := "2006-01-02T15:04:05Z"
	t, _ := time.Parse(layout, s)
	return t.Before(u)
}

// GITHUB REQUEST

type GithubIssueRequestParameters map[GithubIssueRequestParameter]string
type GithubIssueRequestParameter string

const (
	IssueReqParamLabels  GithubIssueRequestParameter = "labels"
	IssueReqParamSort    GithubIssueRequestParameter = "sort"
	IssueReqParamSince   GithubIssueRequestParameter = "since"
	IssueReqParamPerpage GithubIssueRequestParameter = "per_page"
	IssueReqParamPage    GithubIssueRequestParameter = "page"
)

// GithubIssueRequest used to define how to gather github issue information
type GithubIssueRequest struct {
	Owner     string
	Repo      string
	Params    GithubIssueRequestParameters
	AuthToken string
}

// GITHUB ISSUES

// GithubIssues contains multiple GithubIssueElement
type GithubIssues []GithubIssueElement

// GithubIssuesAfterId issue id points to GithubIssueElement
type GithubIssuesAfterId map[int64]GithubIssueElement

// UnmarshalGithubIssue transforms []byte into GithubIssues
func UnmarshalGithubIssue(data []byte) (GithubIssues, error) {
	var r GithubIssues
	err := json.Unmarshal(data, &r)
	return r, err
}

// Marshal transformes GithubIssues into []byte
func (r *GithubIssues) Marshal() ([]byte, error) {
	return json.Marshal(r)
}

// GithubIssueElement github issue information
type GithubIssueElement struct {
	HTMLURL   string     `json:"html_url"`
	Number    int64      `json:"number"`
	Title     string     `json:"title"`
	Labels    []Label    `json:"labels"`
	State     string     `json:"state"`
	Milestone *Milestone `json:"milestone"`
	Comments  int64      `json:"comments"`
	CreatedAt string     `json:"created_at"`
	UpdatedAt string     `json:"updated_at"`
	ClosedAt  string     `json:"closed_at"`
}

// Label github label
type Label struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

// Milestone github milestone
type Milestone struct {
	Title string `json:"title"`
}
