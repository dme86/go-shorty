package meta

import (
	"reflect"
	"testing"

	"github.com/example/shorty/internal/store"
)

func TestInferTagsIsDeterministicAndPrioritizesHost(t *testing.T) {
	url:="https://www.youtube.com/workshop?q=golang+distributed+architecture&tags=rust+cloud+systems"
	md:=store.Meta{Title:"Reliable programming with Linux Kubernetes Observability Security",Description:"Open source metrics and tracing"}
	var first []string
	for i:=0;i<500;i++ {
		got:=InferTags(url,md)
		if len(got)!=8 {t.Fatalf("got %d tags: %v",len(got),got)}
		if got[0]!="youtube"||got[1]!="video" {
			t.Fatalf("host/service tags must be first: %v",got)
		}
		if i==0 {first=got} else if !reflect.DeepEqual(first,got) {
			t.Fatalf("nondeterministic tags: first %v now %v",first,got)
		}
		seen:=map[string]bool{}
		for _,tag:=range got {
			if seen[tag] { t.Fatalf("duplicate tag: %s",tag) }
			seen[tag]=true
		}
	}
}
