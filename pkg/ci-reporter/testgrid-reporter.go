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
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// TestgridReport used to implement RequestData & Print for testgrid report data
type TestgridReport struct {
	ReportData ReportData
}

// RequestTestgridOverview this function is used to accumulate a summary of testgrid
func (r *TestgridReport) RequestData(meta Meta, wg *sync.WaitGroup) ReportData {
	// The report checks master-blocking and master-informing
	requiredJobs := []testgridJob{
		{OutputName: "Master-Blocking", URLName: "sig-release-master-blocking", Emoji: masterBlockingEmoji},
		{OutputName: "Master-Informing", URLName: "sig-release-master-informing", Emoji: masterInformingEmoji},
	}

	// If a release version got specified add additional jobs to report
	if len(meta.Flags.ReleaseVersion) > 0 {
		for _, r := range meta.Flags.ReleaseVersion {
			requiredJobs = append(requiredJobs, testgridJob{OutputName: fmt.Sprintf("%s-blocking", r), URLName: fmt.Sprintf("sig-release-%s-blocking", r), Emoji: masterBlockingEmoji})
			requiredJobs = append(requiredJobs, testgridJob{OutputName: fmt.Sprintf("%s-informing", r), URLName: fmt.Sprintf("sig-release-%s-informing", r), Emoji: masterInformingEmoji})
		}
	}

	return meta.DataPostProcessing(r, "testgrid", assembleTestgridRequests(meta, requiredJobs), wg)
}

// Print extends TestgridReport and prints report data to the console
func (r *TestgridReport) Print(meta Meta, reportData ReportData) {
	for _, reportField := range reportData.Data {
		headerLine := fmt.Sprintf("\n\n%s Tests in %s", reportField.Emoji, reportField.Title)
		if meta.Flags.EmojisOff {
			headerLine = fmt.Sprintf("\n\nTests in %s", reportField.Title)
		}
		for _, stat := range reportField.Records {
			if stat.ID == testgridReportSummary {
				fmt.Println(headerLine)
				for _, note := range stat.Notes {
					fmt.Println("- " + note)
				}
				fmt.Print("\n")
				if !meta.Flags.ShortOn {
					fmt.Println("Job details:")
				}
			} else if stat.ID == testgridReportDetails {
				if meta.Flags.EmojisOff {
					fmt.Printf("%s severity:%d, %s\n", stat.Status, stat.Severity, stat.Title)
				} else {
					fmt.Printf("%s %s %s\n", stat.Status, stat.Highlight, stat.Title)
				}
				fmt.Printf("- %s\n", stat.URL)
				for _, note := range stat.Notes {
					fmt.Printf("- %s\n", note)
				}
			}
		}
	}
}

// PutData extends TestgridReport and stores the data at runtime to the struct val ReportData
func (r *TestgridReport) PutData(reportData ReportData) {
	r.ReportData = reportData
}

// GetData extends TestgridReport and returns the data that has been stored at runtime int the struct val ReportData (counter to SaveData/1)
func (r TestgridReport) GetData() ReportData {
	return r.ReportData
}

func assembleTestgridRequests(meta Meta, requiredJobs []testgridJob) chan ReportDataField {
	c := make(chan ReportDataField)
	go func() {
		defer close(c)
		wg := sync.WaitGroup{}
		for _, j := range requiredJobs {
			wg.Add(1)
			go func(job testgridJob) {
				jobBaseUrl := fmt.Sprintf("https://testgrid.k8s.io/%s", job.URLName)
				jobsData, err := reqTestgridSiteData(job, jobBaseUrl)
				if err != nil {
					log.Fatalf("error %v", err)
				}
				records := []ReportDataRecord{getSummary(jobsData)}

				if !meta.Flags.ShortOn {
					for jobName, jobData := range jobsData {
						if jobData.OverallStatus != Passing {
							records = append(records, getDetails(jobName, jobData, jobBaseUrl, meta.Flags.EmojisOff))
						}
					}
				}

				reportData := ReportDataField{
					Emoji:   job.Emoji,
					Title:   job.OutputName,
					Records: records,
				}
				c <- reportData
				wg.Done()
			}(j)
		}
		wg.Wait()
	}()
	return c
}

// This function is used to request job summary data from a testgrid subpage
func reqTestgridSiteData(job testgridJob, jobBaseUrl string) (TestgridData, error) {
	// This url points to testgrid/summary which returns a JSON document
	url := fmt.Sprintf("%s/summary", jobBaseUrl)
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	// Parse body form http request
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	// Unmarshal JSON from body into TestgridJobsOverview struct
	jobs, err := UnmarshalTestgrid(body)
	if err != nil {
		return nil, err
	}
	return jobs, nil
}

// This function is used to count up the status from testgrid tests
func getSummary(jobs map[string]TestgridValue) ReportDataRecord {
	result := ReportDataRecord{ID: testgridReportSummary}
	statuses := map[OverallStatus]int{Total: len(jobs), Passing: 0, Failing: 0, Flaky: 0, Stale: 0}
	for _, v := range jobs {
		if v.OverallStatus == Passing {
			statuses[Passing]++
		} else if v.OverallStatus == Failing {
			statuses[Failing]++
		} else if v.OverallStatus == Flaky {
			statuses[Flaky]++
		} else {
			statuses[Stale]++
		}
	}
	result.Notes = append(result.Notes, fmt.Sprintf("%d jobs %s", statuses[Total], strings.ToLower(string(Total))))
	result.Notes = append(result.Notes, fmt.Sprintf("%d jobs %s", statuses[Passing], strings.ToLower(string(Passing))))
	result.Notes = append(result.Notes, fmt.Sprintf("%d jobs %s", statuses[Flaky], strings.ToLower(string(Flaky))))
	result.Notes = append(result.Notes, fmt.Sprintf("%d jobs %s", statuses[Failing], strings.ToLower(string(Failing))))
	if statuses[Stale] != 0 {
		result.Notes = append(result.Notes, fmt.Sprintf("%d jobs %s", statuses[Stale], strings.ToLower(string(Stale))))
	}
	return result
}

// This function is used get additional information about testgrid jobs
func getDetails(jobName string, jobData TestgridValue, jobBaseUrl string, emojisOff bool) ReportDataRecord {
	result := ReportDataRecord{ID: testgridReportDetails}
	result.Status = string(jobData.OverallStatus)
	result.Title = jobName
	result.URL = fmt.Sprintf("%s#%s", jobBaseUrl, jobName)

	// If the status is failing give information about failing tests
	if jobData.OverallStatus == Failing {
		// Filter sigs
		sigRegex := regexp.MustCompile(`sig-[a-zA-Z]+`)
		sigsInvolved := map[string]int{}
		for _, test := range jobData.Tests {
			sigs := sigRegex.FindAllString(test.TestName, -1)
			for _, sig := range sigs {
				sigsInvolved[sig] = sigsInvolved[sig] + 1
			}
		}
		sigs := reflect.ValueOf(sigsInvolved).MapKeys()

		result.Notes = append(result.Notes, fmt.Sprintf("Sig's involved %v", sigs))
		result.Notes = append(result.Notes, fmt.Sprintf("Currently %d test are failing", len(jobData.Tests)))

		// result.Notes = append(result.Notes, fmt.Sprintf("%d/%d tests passed on the last run, %d/%d are new tests that have never passed yet", numberOfPassingTestsAfterFailure, amountOfFailingTests, numberOfNewTestsThatNeverPassedYet, amountOfFailingTests))
	}

	const (
		testgridRegexRecentRuns   = "runs"
		testgridRegexRecentPasses = "passes"

		thresholdWarning  float64 = 0.5 // 0.0 ... 0.5 -> warning
		thresholdInfo     float64 = 0.8 // 0.5 ... 0.8 -> info
		newTestThreshhold float64 = 5.0 // if 0.0 ... 5.0 -> new test
	)
	// This regex filters the latest executions
	// e.g. "8 of 9 (88.9%) recent columns passed (19455 of 19458 or 100.0% cells)" -> 8 passes of 9 runs recently
	latestExec := getRegexParams(fmt.Sprintf(`(?P<%s>\d{1,2})\sof\s(?P<%s>\d{1,2})`, testgridRegexRecentPasses, testgridRegexRecentRuns), jobData.Status)
	testgridRegexRecentPassesFloat, err := strconv.ParseFloat(latestExec[testgridRegexRecentPasses], 64)
	if err != nil {
		fmt.Println(err)
	}
	testgridRegexRecentRunsFloat, err := strconv.ParseFloat(latestExec[testgridRegexRecentRuns], 64)
	if err != nil {
		fmt.Println(err)
	}

	highlightEmoji := ""
	if jobData.OverallStatus == Failing {
		highlightEmoji = statusFailingEmoji
	} else {
		highlightEmoji = statusFlakyEmoji
	}
	recentSuccessRate := testgridRegexRecentPassesFloat / testgridRegexRecentRunsFloat
	severity := Severity(0)
	if testgridRegexRecentRunsFloat <= newTestThreshhold {
		severity = LightSeverity
		highlightEmoji = statusNewTestEmoji
	} else {
		if recentSuccessRate <= thresholdWarning {
			severity = HighSeverity
		} else if recentSuccessRate <= thresholdInfo {
			severity = MediumSeverity
		} else {
			severity = LightSeverity
		}
	}

	result.Severity = severity
	result.Highlight = strings.Repeat(highlightEmoji, int(severity))

	result.Notes = append(result.Notes, fmt.Sprintf("%s of %s passed recently", latestExec[testgridRegexRecentPasses], latestExec[testgridRegexRecentRuns]))
	return result
}

// Parses string with the given regular expression and returns the group values defined in the expression.
// e.g. `(?P<Year>\d{4})-(?P<Month>\d{2})-(?P<Day>\d{2})` + `2015-05-27` -> map[Year:2015 Month:05 Day:27]
func getRegexParams(regEx, s string) (paramsMap map[string]string) {
	var compRegEx = regexp.MustCompile(regEx)
	match := compRegEx.FindStringSubmatch(s)

	paramsMap = make(map[string]string)
	for i, name := range compRegEx.SubexpNames() {
		if i > 0 && i <= len(match) {
			paramsMap[name] = match[i]
		}
	}
	return paramsMap
}

type testgridJob struct {
	OutputName string
	URLName    string
	Emoji      string
}

// The types below reflect testgrid summary json (e.g. https://testgrid.k8s.io/sig-release-master-informing/summary)

// TestgridData contains all jobs under one specific field like 'sig-release-master-informing'
type TestgridData map[string]TestgridValue

// UnmarshalTestgrid []byte into TestgridData
func UnmarshalTestgrid(data []byte) (TestgridData, error) {
	var r TestgridData
	err := json.Unmarshal(data, &r)
	return r, err
}

// Marshal TestgridData struct into []bytes
func (r *TestgridData) Marshal() ([]byte, error) {
	return json.Marshal(r)
}

// TestgridValue information about a specifc job
type TestgridValue struct {
	Alert               string            `json:"alert"`
	LastRunTimestamp    int64             `json:"last_run_timestamp"`
	LastUpdateTimestamp int64             `json:"last_update_timestamp"`
	LatestGreen         string            `json:"latest_green"`
	OverallStatus       OverallStatus     `json:"overall_status"`
	OverallStatusIcon   OverallStatusIcon `json:"overall_status_icon"`
	Status              string            `json:"status"`
	Tests               []Test            `json:"tests"`
	DashboardName       DashboardName     `json:"dashboard_name"`
	Healthiness         Healthiness       `json:"healthiness"`
	BugURL              string            `json:"bug_url"`
}

// Healthiness
type Healthiness struct {
	Tests             []interface{} `json:"tests"`
	PreviousFlakiness int64         `json:"previousFlakiness"`
}

type Test struct {
	DisplayName    string        `json:"display_name"`
	TestName       string        `json:"test_name"`
	FailCount      int64         `json:"fail_count"`
	FailTimestamp  int64         `json:"fail_timestamp"`
	PassTimestamp  int64         `json:"pass_timestamp"`
	BuildLink      string        `json:"build_link"`
	BuildURLText   string        `json:"build_url_text"`
	BuildLinkText  string        `json:"build_link_text"`
	FailureMessage string        `json:"failure_message"`
	LinkedBugs     []interface{} `json:"linked_bugs"`
	FailTestLink   string        `json:"fail_test_link"`
}

type DashboardName string

const (
	SigReleaseMasterInforming DashboardName = "sig-release-master-informing"
	SigReleaseMasterBlocking  DashboardName = "sig-release-master-blocking"
)

type OverallStatus string

const (
	Total   OverallStatus = "TOTAL"
	Failing OverallStatus = "FAILING"
	Flaky   OverallStatus = "FLAKY"
	Passing OverallStatus = "PASSING"
	Stale   OverallStatus = "STALE"
)

type OverallStatusIcon string

const (
	Done                OverallStatusIcon = "done"
	RemoveCircleOutline OverallStatusIcon = "remove_circle_outline"
	Warning             OverallStatusIcon = "warning"
)

// This information is used internally to differentiate between summary and detail ReportDataRecords
const (
	testgridReportSummary = 0
	testgridReportDetails = 1
)
