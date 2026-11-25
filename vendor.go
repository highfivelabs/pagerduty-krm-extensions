// Copyright 2026 High Five Labs
// SPDX-License-Identifier: Apache-2.0

package extensions

import (
	"net/http"

	"github.com/highfivelabs/krm-rest-runtime/rest"
)

// Vendor is how the manager reaches PagerDuty.
var Vendor = &rest.Vendor{
	Name:           "pagerduty",
	DefaultBaseURL: "https://api.pagerduty.com",
	CredentialKeys: []string{"token"},
	Authorize:      func(r *http.Request, c map[string]string) { r.Header.Set("Authorization", "Token token="+c["token"]) },
	Headers:        map[string]string{"Accept": "application/vnd.pagerduty+json;version=2"},
}
