package registry

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/remind101/empire/pkg/dockerutil"
	"github.com/remind101/empire/pkg/httpmock"
	"github.com/remind101/empire/pkg/image"
	"github.com/remind101/empire/pkg/jsonmessage"
)

func TestResolve(t *testing.T) {
	api := httpmock.NewServeReplay(t).Add(httpmock.PathHandler(t,
		"GET /version",
		200, `{ "ApiVersion": "1.20" }`,
	)).Add(httpmock.PathHandler(t,
		"GET /images/remind101:acme-inc/json",
		200, `{ "RepoDigests": [ "remind101/acme-inc@sha256:c6f77d2098bc0e32aef3102e71b51831a9083dd9356a0ccadca860596a1e9007" ] }`,
	))

	c, s := newTestDockerClient(t, api)
	defer s.Close()

	d := DockerDaemonRegistry{
		docker: c,
		noPull: true,
	}

	w := jsonmessage.NewStream(ioutil.Discard)
	img, err := d.Resolve(nil, image.Image{
		Tag:        "acme-inc",
		Repository: "remind101",
	}, w)
	if err != nil {
		t.Fatal(err)
	}

	if got, want := img, "remind101/acme-inc@sha256:c6f77d2098bc0e32aef3102e71b51831a9083dd9356a0ccadca860596a1e9007"; got.String() != want {
		t.Fatalf("Resolve() => %s; want %s", got, want)
	}

	img, err = d.Resolve(nil, img, w)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := img, "remind101/acme-inc@sha256:c6f77d2098bc0e32aef3102e71b51831a9083dd9356a0ccadca860596a1e9007"; got.String() != want {
		t.Fatalf("Resolve() => %s; want %s", got, want)
	}
}

func TestResolve_NoDigest_WithDigestsPrefer(t *testing.T) {
	api := httpmock.NewServeReplay(t).Add(httpmock.PathHandler(t,
		"GET /version",
		200, `{ "ApiVersion": "1.20" }`,
	)).Add(httpmock.PathHandler(t,
		"GET /images/remind101:acme-inc/json",
		200, `{ "RepoDigests": [] }`,
	))

	c, s := newTestDockerClient(t, api)
	defer s.Close()

	d := DockerDaemonRegistry{
		docker: c,
		noPull: true,
	}

	w := jsonmessage.NewStream(ioutil.Discard)
	img, err := d.Resolve(nil, image.Image{
		Tag:        "acme-inc",
		Repository: "remind101",
	}, w)
	if err != nil {
		t.Fatal(err)
	}

	if got, want := img, "remind101:acme-inc"; got.String() != want {
		t.Fatalf("Resolve() => %s; want %s", got, want)
	}
}

func TestResolve_NoDigest_WithDigestsOnly(t *testing.T) {
	api := httpmock.NewServeReplay(t).Add(httpmock.PathHandler(t,
		"GET /version",
		200, `{ "ApiVersion": "1.20" }`,
	)).Add(httpmock.PathHandler(t,
		"GET /images/remind101:acme-inc/json",
		200, `{ "RepoDigests": [] }`,
	))

	c, s := newTestDockerClient(t, api)
	defer s.Close()

	d := DockerDaemonRegistry{
		Digests: DigestsOnly,
		docker:  c,
		noPull:  true,
	}

	w := jsonmessage.NewStream(ioutil.Discard)
	_, err := d.Resolve(nil, image.Image{
		Tag:        "acme-inc",
		Repository: "remind101",
	}, w)
	if err == nil {
		t.Fatal(fmt.Errorf("expected an error"))
	}
}

func TestResolve_NoDigest_WithDigestsDisable(t *testing.T) {
	api := httpmock.NewServeReplay(t).Add(httpmock.PathHandler(t,
		"GET /version",
		200, `{ "ApiVersion": "1.20" }`,
	))

	c, s := newTestDockerClient(t, api)
	defer s.Close()

	d := DockerDaemonRegistry{
		Digests: DigestsDisable,
		docker:  c,
		noPull:  true,
	}

	w := jsonmessage.NewStream(ioutil.Discard)
	img, err := d.Resolve(nil, image.Image{
		Tag:        "acme-inc",
		Repository: "remind101",
	}, w)
	if err != nil {
		t.Fatal(err)
	}

	if got, want := img, "remind101:acme-inc"; got.String() != want {
		t.Fatalf("Resolve() => %s; want %s", got, want)
	}
}

func TestCMDExtractor(t *testing.T) {
	api := httpmock.NewServeReplay(t).Add(httpmock.PathHandler(t,
		"GET /version",
		200, `{ "ApiVersion": "1.20" }`,
	)).Add(httpmock.PathHandler(t,
		"GET /images/remind101:acme-inc/json",
		200, `{ "Config": { "Cmd": ["/go/bin/app","server"] } }`,
	))

	c, s := newTestDockerClient(t, api)
	defer s.Close()

	e := cmdExtractor{
		client: c,
	}

	w := jsonmessage.NewStream(ioutil.Discard)
	got, err := e.ExtractProcfile(nil, image.Image{
		Tag:        "acme-inc",
		Repository: "remind101",
	}, w)
	if err != nil {
		t.Fatal(err)
	}

	want := []byte(`web:
  command:
  - /go/bin/app
  - server
`)

	if !reflect.DeepEqual(got, want) {
		t.Errorf("ExtractProcfile() => %q; want %q", got, want)
	}
}

func TestProcfileExtractor(t *testing.T) {
	api := httpmock.NewServeReplay(t).Add(httpmock.PathHandler(t,
		"GET /version",
		200, `{ "ApiVersion": "1.20" }`,
	)).Add(httpmock.PathHandler(t,
		"POST /containers/create",
		200, `{ "ID": "abc" }`,
	)).Add(httpmock.PathHandler(t,
		"GET /containers/abc/json",
		200, `{}`,
	)).Add(httpmock.PathHandler(t,
		"POST /containers/abc/copy",
		200, tarProcfile(t),
	)).Add(httpmock.PathHandler(t,
		"DELETE /containers/abc",
		200, `{}`,
	))

	c, s := newTestDockerClient(t, api)
	defer s.Close()

	e := fileExtractor{
		client: c,
	}

	w := jsonmessage.NewStream(ioutil.Discard)
	got, err := e.ExtractProcfile(nil, image.Image{
		Tag:        "acme-inc",
		Repository: "remind101",
	}, w)
	if err != nil {
		t.Fatal(err)
	}

	want := []byte(`web: rails server`)

	if !reflect.DeepEqual(got, want) {
		t.Errorf("ExtractProcfile() => %q; want %q", got, want)
	}
}

func TestProcfileExtractor_Docker12(t *testing.T) {
	api := httpmock.NewServeReplay(t).Add(httpmock.PathHandler(t,
		"GET /version",
		200, `{ "ApiVersion": "1.24" }`,
	)).Add(httpmock.PathHandler(t,
		"POST /containers/create",
		200, `{ "ID": "abc" }`,
	)).Add(httpmock.PathHandler(t,
		"GET /containers/abc/json",
		200, `{}`,
	)).Add(httpmock.PathHandler(t,
		"GET /containers/abc/archive",
		200, tarProcfile(t),
	)).Add(httpmock.PathHandler(t,
		"DELETE /containers/abc",
		200, `{}`,
	))

	c, s := newTestDockerClient(t, api)
	defer s.Close()

	e := fileExtractor{
		client: c,
	}

	w := jsonmessage.NewStream(ioutil.Discard)
	got, err := e.ExtractProcfile(nil, image.Image{
		Tag:        "acme-inc",
		Repository: "remind101",
	}, w)
	if err != nil {
		t.Fatal(err)
	}

	want := []byte(`web: rails server`)

	if !reflect.DeepEqual(got, want) {
		t.Errorf("ExtractProcfile() => %q; want %q", got, want)
	}
}

func TestProcfileFallbackExtractor(t *testing.T) {
	api := httpmock.NewServeReplay(t).Add(httpmock.PathHandler(t,
		"GET /version",
		200, `{ "ApiVersion": "1.20" }`,
	)).Add(httpmock.PathHandler(t,
		"POST /containers/create",
		200, `{ "ID": "abc" }`,
	)).Add(httpmock.PathHandler(t,
		"GET /containers/abc/json",
		200, `{}`,
	)).Add(httpmock.PathHandler(t,
		"POST /containers/abc/copy",
		404, ``,
	)).Add(httpmock.PathHandler(t,
		"DELETE /containers/abc",
		200, `{}`,
	)).Add(httpmock.PathHandler(t,
		"GET /images/remind101:acme-inc/json",
		200, `{ "Config": { "Cmd": ["/go/bin/app","server"] } }`,
	))

	c, s := newTestDockerClient(t, api)
	defer s.Close()

	e := multiExtractor(
		newFileExtractor(c),
		newCMDExtractor(c),
	)

	w := jsonmessage.NewStream(ioutil.Discard)
	got, err := e.ExtractProcfile(nil, image.Image{
		Tag:        "acme-inc",
		Repository: "remind101",
	}, w)
	if err != nil {
		t.Fatal(err)
	}

	want := []byte(`web:
  command:
  - /go/bin/app
  - server
`)

	if !reflect.DeepEqual(got, want) {
		t.Errorf("ExtractProcfile() => %q; want %q", got, want)
	}

}

// newTestDockerClient returns a docker.Client configured to talk to the given http.Handler
func newTestDockerClient(t *testing.T, fakeDockerAPI http.Handler) (*dockerutil.Client, *httptest.Server) {
	s := httptest.NewServer(fakeDockerAPI)

	c, err := dockerutil.NewClient(nil, s.URL, "")
	if err != nil {
		t.Fatal(err)
	}

	return c, s
}

func tarProcfile(t *testing.T) string {
	buf := new(bytes.Buffer)
	tw := tar.NewWriter(buf)

	var files = []struct {
		Name, Body string
	}{
		{"Procfile", "web: rails server"},
	}

	for _, file := range files {
		hdr := &tar.Header{
			Name: file.Name,
			Size: int64(len(file.Body)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(file.Body)); err != nil {
			t.Fatal(err)
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	return buf.String()
}
