// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-present Datadog, Inc.

package ec2

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	configmock "github.com/DataDog/datadog-agent/pkg/config/mock"
	"github.com/DataDog/datadog-agent/pkg/util/dmi"
	httputils "github.com/DataDog/datadog-agent/pkg/util/http"
)

var (
	initialMetadataURL = metadataURL
	initialTokenURL    = tokenURL
)

const testIMDSToken = "AQAAAFKw7LyqwVmmBMkqXHpDBuDWw2GnfGswTHi2yiIOGvzD7OMaWw=="

func resetPackageVars() {
	metadataURL = initialMetadataURL
	tokenURL = initialTokenURL
	token = httputils.NewAPIToken(getToken)
	currentMetadataSource = metadataSourceNone

	instanceIDFetcher.Reset()
	publicIPv4Fetcher.Reset()
	hostnameFetcher.Reset()
	networkIDFetcher.Reset()
}

func setupDMIForEC2(t *testing.T) {
	dmi.SetupMock(t, "ec2something", "ec2something2", "i-myinstance", DMIBoardVendor)
}

func setupDMIForNotEC2(t *testing.T) {
	dmi.SetupMock(t, "", "", "", "")
}

func TestIsDefaultHostname(t *testing.T) {
	conf := configmock.New(t)

	for _, prefix := range []bool{true, false} {
		conf.SetDefault("ec2_use_windows_prefix_detection", prefix)

		assert.True(t, IsDefaultHostname("IP-FOO"))
		assert.True(t, IsDefaultHostname("domuarigato"))
		assert.Equal(t, prefix, IsDefaultHostname("EC2AMAZ-FOO"))
		assert.False(t, IsDefaultHostname(""))
	}
}

func TestIsDefaultHostnameForIntake(t *testing.T) {
	conf := configmock.New(t)
	conf.SetDefault("ec2_use_windows_prefix_detection", true)

	assert.True(t, IsDefaultHostnameForIntake("IP-FOO"))
	assert.True(t, IsDefaultHostnameForIntake("domuarigato"))
	assert.False(t, IsDefaultHostnameForIntake("EC2AMAZ-FOO"))
	assert.True(t, IsDefaultHostname("EC2AMAZ-FOO"))
}

func TestGetInstanceID(t *testing.T) {
	ctx := context.Background()
	var expected string
	var responseCode int
	var lastRequest *http.Request

	// Force refresh
	token.ExpirationDate = time.Now()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		switch r.Method {
		case http.MethodPut:
			// Should be a token request
			io.WriteString(w, testIMDSToken)
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			// Should be a metadata request
			t := r.Header.Get("X-aws-ec2-metadata-token")
			if t != testIMDSToken {
				w.WriteHeader(http.StatusUnauthorized)
			}
			io.WriteString(w, expected)
			w.WriteHeader(responseCode)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
		lastRequest = r
	}))
	defer ts.Close()
	metadataURL = ts.URL
	tokenURL = ts.URL
	conf := configmock.New(t)
	defer resetPackageVars()
	conf.SetWithoutSource("ec2_metadata_timeout", 1000)

	// API successful, should return API result
	responseCode = http.StatusOK
	expected = "i-0123456789abcdef0"
	val, err := GetInstanceID(ctx)
	assert.NoError(t, err)
	assert.Equal(t, expected, val)
	assert.Equal(t, lastRequest.URL.Path, "/instance-id")

	// the internal cache is populated now, should return the cached value even if API errors out
	responseCode = http.StatusInternalServerError
	val, err = GetInstanceID(ctx)
	assert.NoError(t, err)
	assert.Equal(t, expected, val)
	assert.Equal(t, lastRequest.URL.Path, "/instance-id")

	// the internal cache is populated, should refresh result if API call succeeds
	responseCode = http.StatusOK
	expected = "i-aaaaaaaaaaaaaaaaa"
	val, err = GetInstanceID(ctx)
	assert.NoError(t, err)
	assert.Equal(t, expected, val)
	assert.Equal(t, lastRequest.URL.Path, "/instance-id")
}

func TestGetLegacyResolutionInstanceID(t *testing.T) {
	ctx := context.Background()
	expected := "i-0123456789abcdef0"
	var responseCode int
	var lastRequest *http.Request
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(responseCode)
		io.WriteString(w, expected)
		lastRequest = r
	}))
	defer ts.Close()
	metadataURL = ts.URL
	conf := configmock.New(t)
	defer resetPackageVars()
	conf.SetWithoutSource("ec2_metadata_timeout", 1000)

	// API errors out, should return error
	responseCode = http.StatusInternalServerError
	val, err := GetLegacyResolutionInstanceID(ctx)
	assert.NotNil(t, err)
	assert.Equal(t, "", val)
	assert.Equal(t, lastRequest.URL.Path, "/instance-id")

	// API successful, should return API result
	responseCode = http.StatusOK
	val, err = GetLegacyResolutionInstanceID(ctx)
	assert.NoError(t, err)
	assert.Equal(t, expected, val)
	assert.Equal(t, lastRequest.URL.Path, "/instance-id")

	// the internal cache is populated now, should return the cached value even if API errors out
	responseCode = http.StatusInternalServerError
	val, err = GetLegacyResolutionInstanceID(ctx)
	assert.NoError(t, err)
	assert.Equal(t, expected, val)
	assert.Equal(t, lastRequest.URL.Path, "/instance-id")

	// the internal cache is populated, should refresh result if API call succeeds
	responseCode = http.StatusOK
	expected = "i-aaaaaaaaaaaaaaaaa"
	val, err = GetLegacyResolutionInstanceID(ctx)
	assert.NoError(t, err)
	assert.Equal(t, expected, val)
	assert.Equal(t, lastRequest.URL.Path, "/instance-id")
}

func TestGetHostAliases(t *testing.T) {
	conf := configmock.New(t)
	tests := []struct {
		name          string
		instanceID    string
		expectedHosts []string
		setupDMI      bool
		disableDMI    bool
	}{
		{
			name:          "Instance ID found",
			instanceID:    "i-0b22a22eec53b9321",
			expectedHosts: []string{"i-0b22a22eec53b9321"},
			setupDMI:      false,
		},
		{
			name:          "No Instance ID found",
			expectedHosts: []string{},
			setupDMI:      false,
		},
		{
			name:          "Instance ID found with DMI",
			expectedHosts: []string{"i-myinstance"},
			setupDMI:      true,
		},
		{
			name:          "Instance ID found with DMI",
			expectedHosts: []string{},
			setupDMI:      true,
			disableDMI:    true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.setupDMI {
				setupDMIForEC2(t)
			} else {
				setupDMIForNotEC2(t)
			}

			conf.SetWithoutSource("ec2_use_dmi", !tc.disableDMI)

			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/plain")
				var responseCode int
				if tc.instanceID != "" {
					responseCode = http.StatusOK
				} else {
					responseCode = http.StatusInternalServerError
				}
				w.WriteHeader(responseCode)
				_, _ = io.WriteString(w, tc.instanceID)
			}))
			defer ts.Close()
			defer resetPackageVars()

			metadataURL = ts.URL
			conf.SetWithoutSource("ec2_metadata_timeout", 1000)

			ctx := context.Background()
			aliases, err := GetHostAliases(ctx)
			assert.Equal(t, tc.expectedHosts, aliases)
			assert.NoError(t, err)
		})
	}
}

func TestGetHostname(t *testing.T) {
	ctx := context.Background()
	expected := "ip-10-10-10-10.ec2.internal"
	var responseCode int
	var lastRequest *http.Request
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// save the last request before writing the response to avoid a race when asserting
		lastRequest = r

		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(responseCode)
		io.WriteString(w, expected)
	}))
	defer ts.Close()
	metadataURL = ts.URL

	conf := configmock.New(t)
	defer resetPackageVars()

	conf.SetWithoutSource("ec2_metadata_timeout", 1000)

	// API errors out, should return error
	responseCode = http.StatusInternalServerError
	val, err := GetHostname(ctx)
	assert.NotNil(t, err)
	assert.Equal(t, "", val)
	assert.Equal(t, lastRequest.URL.Path, "/hostname")

	// API successful, should return hostname
	responseCode = http.StatusOK
	val, err = GetHostname(ctx)
	assert.NoError(t, err)
	assert.Equal(t, expected, val)
	assert.Equal(t, lastRequest.URL.Path, "/hostname")

	// the internal cache is populated now, should return the cached hostname even if API errors out
	responseCode = http.StatusInternalServerError
	val, err = GetHostname(ctx)
	assert.NoError(t, err)
	assert.Equal(t, expected, val)
	assert.Equal(t, lastRequest.URL.Path, "/hostname")

	// the internal cache is populated, should refresh result if API call succeeds
	responseCode = http.StatusOK
	expected = "ip-20-20-20-20.ec2.internal"
	val, err = GetHostname(ctx)
	assert.NoError(t, err)
	assert.Equal(t, expected, val)
	assert.Equal(t, lastRequest.URL.Path, "/hostname")

	// clear internal cache
	hostnameFetcher.Reset()

	// ensure we get an empty string along with the error when not on EC2
	metadataURL = "foo"
	val, err = GetHostname(ctx)
	assert.NotNil(t, err)
	assert.Equal(t, "", val)
	assert.Equal(t, lastRequest.URL.Path, "/hostname")
}

func TestExtractClusterName(t *testing.T) {
	testCases := []struct {
		name string
		in   []string
		out  string
		err  error
	}{
		{
			name: "cluster name found",
			in: []string{
				"Name:myclustername-eksnodes-Node",
				"aws:autoscaling:groupName:myclustername-eks-nodes-NodeGroup-11111111",
				"aws:cloudformation:logical-id:NodeGroup",
				"aws:cloudformation:stack-id:arn:aws:cloudformation:zone:1111111111:stack/myclustername-eks-nodes/1111111111",
				"aws:cloudformation:stack-name:myclustername-eks-nodes",
				"kubernetes.io/role/master:1",
				"kubernetes.io/cluster/myclustername:owned",
			},
			out: "myclustername",
			err: nil,
		},
		{
			name: "cluster name not found",
			in: []string{
				"Name:myclustername-eksnodes-Node",
				"aws:autoscaling:groupName:myclustername-eks-nodes-NodeGroup-11111111",
				"aws:cloudformation:logical-id:NodeGroup",
				"aws:cloudformation:stack-id:arn:aws:cloudformation:zone:1111111111:stack/myclustername-eks-nodes/1111111111",
				"aws:cloudformation:stack-name:myclustername-eks-nodes",
				"kubernetes.io/role/master:1",
			},
			out: "",
			err: errors.New("unable to parse cluster name from EC2 tags"),
		},
	}

	for i, test := range testCases {
		t.Run(fmt.Sprintf("case %d: %s", i, test.name), func(t *testing.T) {
			result, err := extractClusterName(test.in)
			assert.Equal(t, test.out, result)
			assert.Equal(t, test.err, err)
		})
	}
}

func TestGetToken(t *testing.T) {
	ctx := context.Background()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		h := r.Header.Get("X-aws-ec2-metadata-token-ttl-seconds")
		if h != "" && r.Method == http.MethodPut {
			io.WriteString(w, testIMDSToken)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	conf := configmock.New(t)
	defer resetPackageVars()

	defer ts.Close()
	tokenURL = ts.URL
	conf.SetWithoutSource("ec2_metadata_timeout", 1000)

	token, err := token.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, testIMDSToken, token)
}

func TestMetedataRequestWithToken(t *testing.T) {
	conf := configmock.New(t)
	testCases := []struct {
		name        string
		configKey   string
		configValue bool
	}{
		{
			name:        "IMDSv2 Preferred",
			configKey:   "ec2_prefer_imdsv2",
			configValue: true,
		},
		{
			name:        "IMDSv2 Transition Payload Enabled",
			configKey:   "ec2_imdsv2_transition_payload_enabled",
			configValue: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var requestWithoutToken *http.Request
			var requestForToken *http.Request
			var requestWithToken *http.Request
			var seq int
			ctx := context.Background()

			ipv4 := "198.51.100.1"

			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/plain")
				switch r.Method {
				case http.MethodPut:
					// Should be a token request
					h := r.Header.Get("X-aws-ec2-metadata-token-ttl-seconds")
					if h == "" {
						w.WriteHeader(http.StatusUnauthorized)
					}
					r.Header.Add("X-sequence", fmt.Sprintf("%v", seq))
					seq++
					requestForToken = r
					io.WriteString(w, testIMDSToken)
				case http.MethodGet:
					// Should be a metadata request
					t := r.Header.Get("X-aws-ec2-metadata-token")
					if t != testIMDSToken {
						r.Header.Add("X-sequence", fmt.Sprintf("%v", seq))
						seq++
						requestWithoutToken = r
						w.WriteHeader(http.StatusUnauthorized)
						return
					}
					switch r.RequestURI {
					case "/public-ipv4":
						r.Header.Add("X-sequence", fmt.Sprintf("%v", seq))
						seq++
						requestWithToken = r
						io.WriteString(w, ipv4)
					default:
						w.WriteHeader(http.StatusNotFound)
					}
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer ts.Close()
			metadataURL = ts.URL
			tokenURL = ts.URL

			// Set test-specific configuration
			defer resetPackageVars()
			conf.SetDefault(tc.configKey, tc.configValue)
			conf.SetWithoutSource("ec2_metadata_timeout", 1000)

			ips, err := GetPublicIPv4(ctx)
			require.NoError(t, err)
			assert.Equal(t, ipv4, ips)

			assert.Nil(t, requestWithoutToken)

			assert.Equal(t, "0", requestForToken.Header.Get("X-sequence"))
			assert.Equal(t, "1", requestWithToken.Header.Get("X-sequence"))
			assert.Equal(t, fmt.Sprint(conf.GetInt("ec2_metadata_token_lifetime")), requestForToken.Header.Get("X-aws-ec2-metadata-token-ttl-seconds"))
			assert.Equal(t, http.MethodPut, requestForToken.Method)
			assert.Equal(t, "/", requestForToken.RequestURI)
			assert.Equal(t, testIMDSToken, requestWithToken.Header.Get("X-aws-ec2-metadata-token"))
			assert.Equal(t, "/public-ipv4", requestWithToken.RequestURI)
			assert.Equal(t, http.MethodGet, requestWithToken.Method)

			// Ensure token has been cached
			ips, err = GetPublicIPv4(ctx)
			require.NoError(t, err)
			assert.Equal(t, ipv4, ips)
			// Unchanged
			assert.Equal(t, "0", requestForToken.Header.Get("X-sequence"))
			// Incremented
			assert.Equal(t, "2", requestWithToken.Header.Get("X-sequence"))

			// Force refresh
			token.ExpirationDate = time.Now()
			ips, err = GetPublicIPv4(ctx)
			require.NoError(t, err)
			assert.Equal(t, ipv4, ips)
			// Incremented
			assert.Equal(t, "3", requestForToken.Header.Get("X-sequence"))
			assert.Equal(t, "4", requestWithToken.Header.Get("X-sequence"))
		})
	}
}

func TestLegacyMetedataRequestWithoutToken(t *testing.T) {
	var requestWithoutToken *http.Request
	conf := configmock.New(t)
	defer resetPackageVars()
	conf.SetDefault("ec2_prefer_imdsv2", false)
	conf.SetDefault("ec2_imdsv2_transition_payload_enabled", false)

	ipv4 := "198.51.100.1"

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		// Put is only use for token
		assert.NotEqual(t, r.Method, http.MethodPut)
		switch r.Method {
		case http.MethodGet:
			// Should be a metadata request without token
			token := r.Header.Get("X-aws-ec2-metadata-token")
			assert.Equal(t, token, "")
			switch r.RequestURI {
			case "/public-ipv4":
				requestWithoutToken = r
				io.WriteString(w, ipv4)
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()
	metadataURL = ts.URL
	tokenURL = ts.URL
	conf.SetWithoutSource("ec2_metadata_timeout", 1000)

	ips, err := GetPublicIPv4(context.Background())
	require.NoError(t, err)
	assert.Equal(t, ipv4, ips)

	assert.Equal(t, "/public-ipv4", requestWithoutToken.RequestURI)
	assert.Equal(t, http.MethodGet, requestWithoutToken.Method)
}

func TestGetNTPHostsFromIMDS(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		io.WriteString(w, "test")
	}))
	defer ts.Close()
	configmock.New(t)
	defer resetPackageVars()

	metadataURL = ts.URL
	actualHosts := GetNTPHosts(context.Background())
	assert.Equal(t, []string{"169.254.169.123"}, actualHosts)
}

func TestGetNTPHostsDMI(t *testing.T) {
	conf := configmock.New(t)
	defer resetPackageVars()
	conf.SetWithoutSource("ec2_use_dmi", true)

	setupDMIForEC2(t)
	metadataURL = ""

	actualHosts := GetNTPHosts(context.Background())
	assert.Equal(t, []string{"169.254.169.123"}, actualHosts)
}

func TestGetNTPHostsEC2UUID(t *testing.T) {
	conf := configmock.New(t)
	defer resetPackageVars()
	conf.SetWithoutSource("ec2_use_dmi", true)

	dmi.SetupMock(t, "ec2something", "", "", "")
	metadataURL = ""

	actualHosts := GetNTPHosts(context.Background())
	assert.Equal(t, []string{"169.254.169.123"}, actualHosts)
}

func TestGetNTPHostsDisabledDMI(t *testing.T) {
	conf := configmock.New(t)
	defer resetPackageVars()
	conf.SetWithoutSource("ec2_use_dmi", false)

	// DMI without EC2 UUID
	dmi.SetupMock(t, "something", "something", "i-myinstance", DMIBoardVendor)
	metadataURL = ""

	actualHosts := GetNTPHosts(context.Background())
	assert.Equal(t, []string(nil), actualHosts)
}

func TestGetNTPHostsNotEC2(t *testing.T) {
	setupDMIForNotEC2(t)
	metadataURL = ""

	actualHosts := GetNTPHosts(context.Background())
	assert.Equal(t, []string(nil), actualHosts)
}

func TestMetadataSourceIMDS(t *testing.T) {
	ctx := context.Background()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		switch r.Method {
		case http.MethodPut: // token request
			io.WriteString(w, testIMDSToken)
		case http.MethodGet: // metadata request
			switch r.RequestURI {
			case "/hostname":
				io.WriteString(w, "ip-10-10-10-10.ec2.internal")
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	metadataURL = ts.URL
	tokenURL = ts.URL
	conf := configmock.New(t)
	defer resetPackageVars()
	conf.SetWithoutSource("ec2_metadata_timeout", 1000)
	conf.SetWithoutSource("ec2_prefer_imdsv2", true)
	conf.SetWithoutSource("ec2_imdsv2_transition_payload_enabled", false)

	assert.True(t, IsRunningOn(ctx))
	assert.Equal(t, metadataSourceIMDSv2, currentMetadataSource)

	hostnameFetcher.Reset()
	currentMetadataSource = metadataSourceNone
	conf.SetWithoutSource("ec2_prefer_imdsv2", false)
	conf.SetWithoutSource("ec2_imdsv2_transition_payload_enabled", true)
	assert.True(t, IsRunningOn(ctx))
	assert.Equal(t, metadataSourceIMDSv2, currentMetadataSource)

	// trying IMDSv1
	hostnameFetcher.Reset()
	currentMetadataSource = metadataSourceNone
	conf.SetWithoutSource("ec2_prefer_imdsv2", false)
	conf.SetWithoutSource("ec2_imdsv2_transition_payload_enabled", false)

	assert.True(t, IsRunningOn(ctx))
	assert.Equal(t, metadataSourceIMDSv1, currentMetadataSource)
}

func TestMetadataSourceUUID(t *testing.T) {
	conf := configmock.New(t)
	defer resetPackageVars()
	conf.SetWithoutSource("ec2_use_dmi", true)

	ctx := context.Background()

	metadataURL = ""

	dmi.SetupMock(t, "ec2something", "", "", "")
	assert.True(t, IsRunningOn(ctx))
	assert.Equal(t, metadataSourceUUID, currentMetadataSource)

	dmi.SetupMock(t, "", "ec2something", "", "")
	assert.True(t, IsRunningOn(ctx))
	assert.Equal(t, metadataSourceUUID, currentMetadataSource)

	dmi.SetupMock(t, "", "45E12AEC-DCD1-B213-94ED-012345ABCDEF", "", "")
	assert.True(t, IsRunningOn(ctx))
	assert.Equal(t, metadataSourceUUID, currentMetadataSource)
}

func TestMetadataSourceDMI(t *testing.T) {
	conf := configmock.New(t)
	defer resetPackageVars()
	conf.SetWithoutSource("ec2_use_dmi", true)

	ctx := context.Background()

	metadataURL = ""

	setupDMIForEC2(t)
	GetHostAliases(ctx)
	assert.Equal(t, metadataSourceDMI, currentMetadataSource)
}

func TestMetadataSourceDMIPreventFallback(t *testing.T) {
	conf := configmock.New(t)
	defer resetPackageVars()
	conf.SetWithoutSource("ec2_use_dmi", true)

	ctx := context.Background()

	metadataURL = ""

	setupDMIForEC2(t)
	GetHostAliases(ctx)
	assert.Equal(t, metadataSourceDMI, currentMetadataSource)

	assert.True(t, IsRunningOn(ctx))
	assert.Equal(t, metadataSourceDMI, currentMetadataSource)
}
