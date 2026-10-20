package main

import (
	"strings"
	"testing"
)

func TestHooksIgnoreLayoutAndFollowForwardedFunction(t *testing.T) {
	s := parseSource([]byte(`package model
 type Sync struct { NewUpstreamField int; Provider int; GenerateConflictDoc bool }
 func NewSync() *Sync {return &Sync{GenerateConflictDoc:false, NewUpstreamField:99, Provider:ProviderSiYuan}}
 func GetCloudRepoSnapshots(page int) int { return snapshotsByTime(page,0,0) }
 func snapshotsByTime(page,start,end int) int {switch provider {case conf.ProviderLocal: return page; default: return 0}}
 `))
	s.field("Sync", "SFTP *SFTP")
	s.defaults("Sync", map[string]string{"Provider": "ProviderS3", "GenerateConflictDoc": "true"})
	s.extendCase("GetCloudRepoSnapshots", "conf.ProviderLocal")
	result := string(s.finish())
	for _, want := range []string{"NewUpstreamField: 99", "GenerateConflictDoc: true", "Provider: ProviderS3", "case conf.ProviderLocal, conf.ProviderSFTP:", "return snapshotsByTime(page, 0, 0)"} {
		if !strings.Contains(result, want) {
			t.Fatalf("missing %s in %s", want, result)
		}
	}
}

func TestAmbiguousProviderHookFailsClosed(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("ambiguous hook must require compatibility review")
		}
	}()
	s := parseSource([]byte(`package model
 func f(){switch first {case conf.ProviderLocal:}; switch second {case conf.ProviderLocal:}}`))
	s.extendCase("f", "conf.ProviderLocal")
}
