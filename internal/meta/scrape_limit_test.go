package meta

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestMetadataHTMLBodyLimits(t *testing.T) {
	makeResponse:=func(contentType,body string,status int,length int64)*http.Response{
		h:=http.Header{}
		if contentType!=""{h.Set("Content-Type",contentType)}
		return &http.Response{StatusCode:status,Header:h,Body:io.NopCloser(strings.NewReader(body)),ContentLength:length}
	}
	const limit=2<<20
	if b,err:=readMetadataHTML(makeResponse("text/html","<title>okay</title>",200,-1));err!=nil||len(b)==0 {
		t.Fatalf("valid metadata rejected: %v",err)
	}
	for _,tc:=range []struct{name string;response *http.Response}{
		{"streaming oversized",makeResponse("text/html",strings.Repeat("a",limit+1),200,-1)},
		{"declared oversized",makeResponse("text/html","short",200,limit+1)},
		{"wrong type",makeResponse("application/octet-stream","data",200,-1)},
		{"HTTP error",makeResponse("text/html","error",503,-1)},
	}{
		t.Run(tc.name,func(t *testing.T){
			defer tc.response.Body.Close()
			if _,err:=readMetadataHTML(tc.response);err==nil {t.Fatal("unsafe metadata response accepted")}
		})
	}
}
