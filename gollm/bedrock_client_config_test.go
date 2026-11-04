// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package gollm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

const (
	bedrockConverseJSON = `{"output":{"message":{"role":"assistant","content":[{"text":"ok"}]}},` +
		`"stopReason":"end_turn","usage":{"inputTokens":1,"outputTokens":1,"totalTokens":2}}`
	stsAssumeRoleXML = `<AssumeRoleResponse xmlns="https://sts.amazonaws.com/doc/2011-06-15/"><AssumeRoleResult>` +
		`<Credentials><AccessKeyId>ROLEKEY</AccessKeyId><SecretAccessKey>role-secret</SecretAccessKey>` +
		`<SessionToken>role-token</SessionToken><Expiration>2099-01-01T00:00:00Z</Expiration></Credentials>` +
		`<AssumedRoleUser><Arn>arn:aws:sts::123456789012:assumed-role/r/s</Arn><AssumedRoleId>AROA:s</AssumedRoleId></AssumedRoleUser>` +
		`</AssumeRoleResult></AssumeRoleResponse>`
)

// signedRequest captures the SigV4 fields of one request.
type signedRequest struct {
	authorization string
	securityToken string
}

// newBedrockStub serves Converse responses and records request signatures.
func newBedrockStub(t *testing.T) (*httptest.Server, func() []signedRequest) {
	t.Helper()
	var mu sync.Mutex
	var reqs []signedRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		reqs = append(reqs, signedRequest{r.Header.Get("Authorization"), r.Header.Get("X-Amz-Security-Token")})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(bedrockConverseJSON))
	}))
	t.Cleanup(srv.Close)
	return srv, func() []signedRequest {
		mu.Lock()
		defer mu.Unlock()
		return append([]signedRequest(nil), reqs...)
	}
}

// setDecoyAWSEnv populates AWS env vars with values the options must override,
// isolates the test from the user's shared config, and routes all AWS calls to
// endpoint so nothing reaches real AWS.
func setDecoyAWSEnv(t *testing.T, endpoint string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(dir, "config"))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(dir, "credentials"))
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_REGION", "eu-west-1")
	t.Setenv("AWS_DEFAULT_REGION", "eu-west-1")
	t.Setenv("AWS_ACCESS_KEY_ID", "ENVKEY")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "env-secret")
	t.Setenv("AWS_SESSION_TOKEN", "env-token")
	t.Setenv("AWS_ENDPOINT_URL", endpoint)
}

func TestBedrockClientConfigOptions(t *testing.T) {
	// stsStub plays STS for the assume-role case.
	stsStub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/xml")
		_, _ = w.Write([]byte(stsAssumeRoleXML))
	}))
	defer stsStub.Close()

	tests := []struct {
		name       string
		opts       []Option
		wantRegion string
		wantKeyID  string
		wantToken  string
	}{
		{
			name:       "env only (unchanged default behaviour)",
			wantRegion: "eu-west-1",
			wantKeyID:  "ENVKEY",
			wantToken:  "env-token",
		},
		{
			name: "static keys with session token",
			opts: []Option{
				WithAWSRegion("us-west-2"),
				WithAWSCredentialsProvider(credentials.NewStaticCredentialsProvider("OPTKEY", "opt-secret", "opt-token")),
			},
			wantRegion: "us-west-2",
			wantKeyID:  "OPTKEY",
			wantToken:  "opt-token",
		},
		{
			name: "assume role",
			opts: []Option{
				WithAWSRegion("ap-southeast-2"),
				WithAWSCredentialsProvider(stscreds.NewAssumeRoleProvider(
					sts.New(sts.Options{
						Region:       "ap-southeast-2",
						BaseEndpoint: aws.String(stsStub.URL),
						Credentials:  credentials.NewStaticCredentialsProvider("BASEKEY", "base-secret", ""),
					}),
					"arn:aws:iam::123456789012:role/r",
				)),
			},
			wantRegion: "ap-southeast-2",
			wantKeyID:  "ROLEKEY",
			wantToken:  "role-token",
		},
		{
			name:       "region only keeps env credentials",
			opts:       []Option{WithAWSRegion("us-west-2")},
			wantRegion: "us-west-2",
			wantKeyID:  "ENVKEY",
			wantToken:  "env-token",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, requests := newBedrockStub(t)
			setDecoyAWSEnv(t, srv.URL)
			ctx := context.Background()

			c, err := NewClient(ctx, "bedrock", tt.opts...)
			if err != nil {
				t.Fatal(err)
			}
			bc := c.(*BedrockClient)

			// Resolved config.
			if got := bc.client.Options().Region; got != tt.wantRegion {
				t.Fatalf("region = %q, want %q", got, tt.wantRegion)
			}
			creds, err := bc.client.Options().Credentials.Retrieve(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if creds.AccessKeyID != tt.wantKeyID || creds.SessionToken != tt.wantToken {
				t.Fatalf("credentials = %q/%q, want %q/%q", creds.AccessKeyID, creds.SessionToken, tt.wantKeyID, tt.wantToken)
			}

			// On the wire: the request is signed with the chosen key, region
			// and session token.
			if _, err := c.StartChat("", "").Send(ctx, "hi"); err != nil {
				t.Fatal(err)
			}
			reqs := requests()
			if len(reqs) == 0 {
				t.Fatal("bedrock stub received no requests")
			}
			wantScope := "Credential=" + tt.wantKeyID + "/"
			wantRegion := "/" + tt.wantRegion + "/bedrock/aws4_request"
			for _, r := range reqs {
				if !strings.Contains(r.authorization, wantScope) || !strings.Contains(r.authorization, wantRegion) {
					t.Fatalf("Authorization %q missing %q or %q", r.authorization, wantScope, wantRegion)
				}
				if r.securityToken != tt.wantToken {
					t.Fatalf("X-Amz-Security-Token = %q, want %q", r.securityToken, tt.wantToken)
				}
			}
		})
	}
}
