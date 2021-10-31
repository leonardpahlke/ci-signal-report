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

import "sync"

// Github card ids
const (
	newCardsID                   = 4212817
	underInvestigationCardsID    = 4212819
	observingCardsID             = 4212821
	githubCiSignalBoardProjectID = 2093513
)

// Emojis
const (
	inFlightEmoji        = "\U0001F6EB"
	notYetStartedEmoji   = "\U0001F914"
	observingEmoji       = "\U0001F440"
	resolvedEmoji        = "\U0001F389"
	masterBlockingEmoji  = "\U000026D4"
	masterInformingEmoji = "\U0001F4A1"
)

type CIReport interface {
	RequestData(meta Meta, wg *sync.WaitGroup) (ReportData, error)
	Print(meta Meta) error
}

type ReportData map[ReportDataField][]ReportDataRecord

type ReportDataField struct {
	Emoji string
	Title string
}

type ReportDataRecord struct {
	Total   int
	Passing int
	Flaking int
	Failing int
	Stale   int

	URL   string
	ID    int64
	Title string
	Sig   string
}
