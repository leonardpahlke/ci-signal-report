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

package main

import (
	"sync"

	ci_reporter "github.com/leonardpahlke/ci-signal-report/pkg/ci-reporter"
)

func main() {
	meta := ci_reporter.SetMeta()
	cireporters := []ci_reporter.CIReport{&ci_reporter.GithubReport{}, &ci_reporter.TestgridReport{}}

	// request report data
	report := ci_reporter.Report{}
	var wg sync.WaitGroup
	for _, r := range cireporters {
		wg.Add(1)
		report = append(report, r.RequestData(meta, &wg))
	}
	wg.Wait()

	// print report data
	if meta.Flags.JsonOut {
		report.PrintJson()
	} else {
		for _, r := range cireporters {
			r.Print(meta, r.GetData())
		}
	}
}
