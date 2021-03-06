package test

import (
	"testing"

	"archive/tar"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"reflect"
	"strings"
	"time"

	docker2aci "github.com/appc/docker2aci/lib"
	d2acommon "github.com/appc/docker2aci/lib/common"
	"github.com/appc/docker2aci/lib/internal/typesV2"
	"github.com/appc/spec/aci"
	"github.com/appc/spec/schema"
	"github.com/appc/spec/schema/types"
)

const variableTestValue = "variant"

var dockerConfig = typesV2.ImageConfig{
	Created:      "2016-06-02T21:43:31.291506236Z",
	Author:       "rkt developer <rkt-dev@googlegroups.com>",
	Architecture: "amd64",
	OS:           "linux",
	Config: &typesV2.ImageConfigConfig{
		User:       "",
		Memory:     12345,
		MemorySwap: 0,
		CpuShares:  9001,
		ExposedPorts: map[string]struct{}{
			"80": struct{}{},
		},
		Env: []string{
			"FOO=1",
		},
		Entrypoint: []string{
			"/bin/sh",
			"-c",
			"echo",
		},
		Cmd: []string{
			"foo",
		},
		Volumes:    nil,
		WorkingDir: "/",
	},
}

func expectedManifest(registryUrl, imageName string) schema.ImageManifest {
	return schema.ImageManifest{
		ACKind:    types.ACKind("ImageManifest"),
		ACVersion: schema.AppContainerVersion,
		Name:      *types.MustACIdentifier("variant"),
		Labels: []types.Label{
			types.Label{
				Name:  *types.MustACIdentifier("arch"),
				Value: "amd64",
			},
			types.Label{
				Name:  *types.MustACIdentifier("os"),
				Value: "linux",
			},
			types.Label{
				Name:  *types.MustACIdentifier("version"),
				Value: "v0.1.0",
			},
		},
		App: &types.App{
			Exec: []string{
				"/bin/sh",
				"-c",
				"echo",
				"foo",
			},
			User:  "0",
			Group: "0",
			Environment: []types.EnvironmentVariable{
				{
					Name:  "FOO",
					Value: "1",
				},
			},
			WorkingDirectory: "/",
			Ports: []types.Port{
				{
					Name:            "80",
					Protocol:        "tcp",
					Port:            80,
					Count:           1,
					SocketActivated: false,
				},
			},
		},
		Annotations: []types.Annotation{
			{
				Name:  *types.MustACIdentifier("author"),
				Value: "rkt developer <rkt-dev@googlegroups.com>",
			},
			{
				Name:  *types.MustACIdentifier("created"),
				Value: "2016-06-02T21:43:31.291506236Z",
			},
			{
				Name:  *types.MustACIdentifier("appc.io/docker/registryurl"),
				Value: registryUrl,
			},
			{
				Name:  *types.MustACIdentifier("appc.io/docker/repository"),
				Value: "docker2aci/dockerv22test",
			},
			{
				Name:  *types.MustACIdentifier("appc.io/docker/imageid"),
				Value: variableTestValue,
				// Different each testrun for unknown reasons
			},
			{
				Name:  *types.MustACIdentifier("appc.io/docker/originalname"),
				Value: imageName,
			},
			{
				Name:  *types.MustACIdentifier("appc.io/docker/entrypoint"),
				Value: "[\"/bin/sh\",\"-c\",\"echo\"]",
			},
			{
				Name:  *types.MustACIdentifier("appc.io/docker/cmd"),
				Value: "[\"foo\"]",
			},
		},
	}
}

func newDocker22Image(layers []Layer) Docker22Image {
	return Docker22Image{
		RepoTags: []string{"testimage:latest"},
		Layers:   layers,
		Config:   dockerConfig,
	}
}

func fetchImage(imgName, outputDir string, squash bool) ([]string, error) {
	conversionTmpDir, err := ioutil.TempDir("", "docker2aci-test-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(conversionTmpDir)

	conf := docker2aci.RemoteConfig{
		CommonConfig: docker2aci.CommonConfig{
			Squash:      squash,
			OutputDir:   outputDir,
			TmpDir:      conversionTmpDir,
			Compression: d2acommon.GzipCompression,
		},
		Username: "",
		Password: "",
		Insecure: d2acommon.InsecureConfig{
			SkipVerify: true,
			AllowHTTP:  true,
		},
	}

	return docker2aci.ConvertRemoteRepo(imgName, conf)
}

func TestFetchingByTagV22(t *testing.T) {
	tmpDir, err := ioutil.TempDir("", "docker2aci-test-")
	if err != nil {
		t.Fatalf("%v", err)
	}
	defer os.RemoveAll(tmpDir)
	layers := []Layer{
		Layer{
			&tar.Header{
				Name:    "thisisafile",
				Mode:    0644,
				ModTime: time.Now(),
			}: []byte("these are its contents"),
		},
	}
	err = GenerateDocker22(tmpDir, newDocker22Image(layers))
	if err != nil {
		t.Fatalf("%v", err)
	}
	imgName := "docker2aci/dockerv22test"
	imgRef := "v0.1.0"
	server := RunDockerRegistry(t, tmpDir, imgName, imgRef, typesV2.MediaTypeDockerV22Manifest)
	defer server.Close()

	bareServerURL := strings.TrimPrefix(server.URL, "http://")
	localUrl := path.Join(bareServerURL, imgName) + ":" + imgRef

	expectedImageManifest := expectedManifest(bareServerURL, localUrl)

	outputDir, err := ioutil.TempDir("", "docker2aci-test-")
	if err != nil {
		t.Fatalf("%v", err)
	}
	defer os.RemoveAll(outputDir)

	acis, err := fetchImage(localUrl, outputDir, true)
	if err != nil {
		t.Fatalf("%v", err)
	}

	converted := acis[0]

	f, err := os.Open(converted)
	if err != nil {
		t.Fatalf("%v", err)
	}
	defer f.Close()

	manifest, err := aci.ManifestFromImage(f)
	if err != nil {
		t.Fatalf("%v", err)
	}

	if err := manifestEqual(manifest, &expectedImageManifest); err != nil {
		t.Fatalf("manifest doesn't match expected manifest: %v", err)
	}
}

func manifestEqual(manifest, expected *schema.ImageManifest) error {
	if manifest.ACKind != expected.ACKind {
		return fmt.Errorf("expected ACKind %q, got %q", expected.ACKind, manifest.ACKind)
	}

	if manifest.ACVersion != expected.ACVersion {
		return fmt.Errorf("expected ACVersion %q, got %q", expected.ACVersion, manifest.ACVersion)
	}

	if !reflect.DeepEqual(*manifest.App, *expected.App) {
		return fmt.Errorf("expected App %v, got %v", *expected.App, *manifest.App)
	}

	if len(manifest.Labels) != len(expected.Labels) {
		return fmt.Errorf("Labels not equal: %v != %v", manifest.Labels, expected.Labels)
	}

	for _, label := range manifest.Labels {
		el, ok := expected.Labels.Get(label.Name.String())
		if !ok {
			return fmt.Errorf("expected label %v to exist, did not", label.Name)
		}
		if label.Value != el {
			return fmt.Errorf("expected label %v values to match, but %v != %v", label.Name, el, label.Value)
		}
	}

	if len(manifest.Annotations) != len(expected.Annotations) {
		return fmt.Errorf("annotations not equal: %v != %v", manifest.Annotations, expected.Annotations)
	}
	for _, ann := range manifest.Annotations {
		ea, ok := expected.Annotations.Get(ann.Name.String())
		if ea == variableTestValue {
			// marker to let us know we don't have to assert on this value; skip it
			continue
		}
		if !ok {
			return fmt.Errorf("expected annotation %v to exist, did not", ann.Name)
		}
		if ea != ann.Value {
			return fmt.Errorf("expected annotation %v values to match, but %v != %v", ann.Name, ea, ann.Value)
		}
	}

	return nil
}

func TestFetchingByDigestV22(t *testing.T) {
	tmpDir, err := ioutil.TempDir("", "docker2aci-test-")
	if err != nil {
		t.Fatalf("%v", err)
	}
	defer os.RemoveAll(tmpDir)
	layers := []Layer{
		Layer{
			&tar.Header{
				Name:    "thisisafile",
				Mode:    0644,
				ModTime: time.Now(),
			}: []byte("these are its contents"),
		},
	}
	err = GenerateDocker22(tmpDir, newDocker22Image(layers))
	if err != nil {
		t.Fatalf("%v", err)
	}
	imgName := "docker2aci/dockerv22test"
	imgRef := "sha256:45b23dee08af5e43a7fea6c4cf9c25ccf269ee113168c19722f87876677c5cb2"
	server := RunDockerRegistry(t, tmpDir, imgName, imgRef, typesV2.MediaTypeDockerV22Manifest)
	defer server.Close()

	localUrl := path.Join(strings.TrimPrefix(server.URL, "http://"), imgName) + "@" + imgRef

	outputDir, err := ioutil.TempDir("", "docker2aci-test-")
	if err != nil {
		t.Fatalf("%v", err)
	}
	defer os.RemoveAll(outputDir)

	_, err = fetchImage(localUrl, outputDir, true)
	if err != nil {
		t.Fatalf("%v", err)
	}
}

func TestFetchingMultipleLayersV22(t *testing.T) {
	tmpDir, err := ioutil.TempDir("", "docker2aci-test-")
	if err != nil {
		t.Fatalf("%v", err)
	}
	defer os.RemoveAll(tmpDir)
	layers := []Layer{
		Layer{
			&tar.Header{
				Name:    "thisisafile",
				Mode:    0644,
				ModTime: time.Now(),
			}: []byte("these are its contents"),
		},
		Layer{
			&tar.Header{
				Name:    "thisisadifferentfile",
				Mode:    0644,
				ModTime: time.Now(),
			}: []byte("the contents of this file are different from the last!"),
		},
	}
	err = GenerateDocker22(tmpDir, newDocker22Image(layers))
	if err != nil {
		t.Fatalf("%v", err)
	}
	imgName := "docker2aci/dockerv22test"
	imgRef := "v0.1.0"
	server := RunDockerRegistry(t, tmpDir, imgName, imgRef, typesV2.MediaTypeDockerV22Manifest)
	defer server.Close()

	localUrl := path.Join(strings.TrimPrefix(server.URL, "http://"), imgName) + ":" + imgRef

	outputDir, err := ioutil.TempDir("", "docker2aci-test-")
	if err != nil {
		t.Fatalf("%v", err)
	}
	defer os.RemoveAll(outputDir)

	_, err = fetchImage(localUrl, outputDir, true)
	if err != nil {
		t.Fatalf("%v", err)
	}
}
