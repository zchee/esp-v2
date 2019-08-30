// Copyright 2018 Google Cloud Platform Proxy Authors
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

package metadata

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"sync"
	"time"

	"cloudesf.googlesource.com/gcpproxy/src/go/util"
	"github.com/golang/glog"

	scpb "cloudesf.googlesource.com/gcpproxy/src/go/proto/api/envoy/http/service_control"
)

var (
	//Suspected Envoy has listener initialization bug: if a http filter needs to use
	//a cluster with DSN lookup for initialization, e.g. fetching a remote access
	//token, the cluster is not ready so the whole listener is destroyed. ADS will
	//repeatedly send the same listener again until the cluster is ready. Then the
	//listener is marked as ready but the whole Envoy server is not marked as ready
	//(worker did not start) somehow. To work around this problem, use IP for
	//metadata server to fetch access token.
	MetadataURL = flag.String("metadata_url", "http://169.254.169.254/computeMetadata", "url of metadata server")
)

const (
	tokenExpiry = 3599
)

type tokenInfo struct {
	accessToken  string
	tokenTimeout time.Time
}

type metadataTokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int64  `json:"expires_in"`
}

type MetadataFetcher struct {
	client  http.Client
	baseUrl string
	timeNow func() time.Time

	mux sync.Mutex
	// metadata updates and stores Metadata from GCE.
	tokenInfo tokenInfo
	// audience -> tokenInfo.
	audToToken sync.Map
}

// Allows for unit tests to inject a mock constructor
var (
	NewMetadataFetcher = func(metadataFetcherTimeout time.Duration) *MetadataFetcher {
		return &MetadataFetcher{
			client: http.Client{
				Timeout: metadataFetcherTimeout,
			},
			baseUrl: *MetadataURL,
			timeNow: time.Now,
		}
	}
)

func (mf *MetadataFetcher) createUrl(suffix string) string {
	return mf.baseUrl + suffix
}

func (mf *MetadataFetcher) getMetadata(path string) ([]byte, error) {
	req, _ := http.NewRequest("GET", path, nil)
	req.Header.Add("Metadata-Flavor", "Google")
	resp, err := mf.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(`failed fetching metadata: %v, status code %v"`, path, resp.StatusCode)
	}
	return ioutil.ReadAll(resp.Body)
}

func (mf *MetadataFetcher) FetchAccessToken() (string, time.Duration, error) {
	now := mf.timeNow()
	// Follow the similar logic as GCE metadata server, where returned token will be valid for at
	// least 60s.
	mf.mux.Lock()
	defer mf.mux.Unlock()
	if mf.tokenInfo.accessToken != "" && !now.After(mf.tokenInfo.tokenTimeout.Add(-time.Second*60)) {
		return mf.tokenInfo.accessToken, mf.tokenInfo.tokenTimeout.Sub(now), nil
	}

	tokenBody, err := mf.getMetadata(mf.createUrl(util.ServiceAccountTokenSuffix))
	if err != nil {
		return "", 0, err
	}

	var resp metadataTokenResponse
	if err = json.Unmarshal(tokenBody, &resp); err != nil {
		return "", 0, err
	}

	expires := time.Duration(resp.ExpiresIn) * time.Second
	mf.tokenInfo.accessToken = resp.AccessToken
	mf.tokenInfo.tokenTimeout = now.Add(expires)
	return mf.tokenInfo.accessToken, expires, nil
}

// TODO(kyuc): perhaps we need some retry logic and timeout?
func (mf *MetadataFetcher) fetchMetadata(key string) (string, error) {
	body, err := mf.getMetadata(mf.createUrl(key))
	if err != nil {
		return "", err
	}

	return string(body), nil
}

func (mf *MetadataFetcher) FetchServiceName() (string, error) {
	return mf.fetchMetadata(util.ServiceNameSuffix)
}

func (mf *MetadataFetcher) FetchConfigId() (string, error) {
	return mf.fetchMetadata(util.ConfigIDSuffix)
}

func (mf *MetadataFetcher) FetchRolloutStrategy() (string, error) {
	return mf.fetchMetadata(util.RolloutStrategySuffix)
}

func (mf *MetadataFetcher) FetchIdentityJWTToken(audience string) (string, time.Duration, error) {
	now := mf.timeNow()
	// Follow the similar logic as GCE metadata server, where returned token will be valid for at
	// least 60s.
	if ti, ok := mf.audToToken.Load(audience); ok {
		info := ti.(tokenInfo)
		if !now.After(info.tokenTimeout.Add(-time.Second * 60)) {
			return info.accessToken, info.tokenTimeout.Sub(now), nil
		}
	}

	identityTokenURI := util.IdentityTokenSuffix + "?audience=" + audience + "&format=standard"
	token, err := mf.fetchMetadata(identityTokenURI)
	if err != nil {
		return "", 0, err
	}

	expires := time.Duration(tokenExpiry) * time.Second
	mf.audToToken.Store(audience, tokenInfo{
		accessToken:  token,
		tokenTimeout: now.Add(expires),
	},
	)
	return token, expires, nil
}

func (mf *MetadataFetcher) FetchGCPAttributes() *scpb.GcpAttributes {
	// Checking if metadata server is reachable.
	if _, err := mf.fetchMetadata(""); err != nil {
		return nil
	}

	attrs := &scpb.GcpAttributes{}
	if projectID, err := mf.FetchProjectId(); err == nil {
		attrs.ProjectId = projectID
	}

	if zone, err := mf.fetchZone(); err == nil {
		attrs.Zone = zone
	}

	attrs.Platform = mf.fetchPlatform()
	return attrs
}

func (mf *MetadataFetcher) FetchProjectId() (string, error) {
	return mf.fetchMetadata(util.ProjectIDSuffix)
}

// Do not directly use this function. Use fetchGCPAttributes instead.
func (mf *MetadataFetcher) fetchZone() (string, error) {
	zonePath, err := mf.fetchMetadata(util.ZoneSuffix)
	if err != nil {
		return "", err
	}

	// Zone format: projects/PROJECT_ID/ZONE
	// Get the substring after the last '/'.
	index := strings.LastIndex(zonePath, "/")
	if index == -1 || index+1 >= len(zonePath) {
		glog.Warningf("Invalid zone format is fetched: %s", zonePath)
		return "", fmt.Errorf("Invalid zone format: %s", zonePath)
	}
	return zonePath[index+1:], nil
}

// Do not directly use this function. Use fetchGCPAttributes instead.
func (mf *MetadataFetcher) fetchPlatform() string {
	if _, err := mf.fetchMetadata(util.GAEServerSoftwareSuffix); err == nil {
		return util.GAEFlex
	}

	if _, err := mf.fetchMetadata(util.KubeEnvSuffix); err == nil {
		return util.GKE
	}

	// TODO(kyuc): what about Cloud Run...?

	return util.GCE
}
