// Copyright 2026 High Five Labs
// SPDX-License-Identifier: Apache-2.0

package extensions

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/yaml"

	"github.com/highfivelabs/krm-rest-runtime/descriptor"
	apifake "github.com/highfivelabs/krm-rest-runtime/fake"
	"github.com/highfivelabs/krm-rest-runtime/reconciler"
)

// HFL-594: the three association kinds POST {"<service|team>": {"id", "type"}}.
// The spec marks both fields read-only, and the generator used to drop them,
// so the kind could not say which service or team it attaches to. This test
// pins the fix: the CRD requires id and defaults type, and the create request
// carries both inside the envelope.
func TestAssocBodyNamesServiceOrTeam(t *testing.T) {
	cases := []struct {
		kind, plural, parent, envelope, refType, wantPath string
	}{
		{"AutomationActionsActionServiceAssociation", "automationactionsactionserviceassociations", "actionID", "service", "service_reference", "/automation_actions/actions/A1/services"},
		{"AutomationActionsActionTeamAssociation", "automationactionsactionteamassociations", "actionID", "team", "team_reference", "/automation_actions/actions/A1/teams"},
		{"AutomationActionsRunnerTeamAssociation", "automationactionsrunnerteamassociations", "runnerID", "team", "team_reference", "/automation_actions/runners/A1/teams"},
	}
	f, err := descriptor.Parse(Types)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			crdSaysIDRequiredAndTypeDefaults(t, tc.plural, tc.refType)

			var k *descriptor.Kind
			for i := range f.Kinds {
				if f.Kinds[i].Kind == tc.kind {
					k = &f.Kinds[i]
				}
			}
			if k == nil {
				t.Fatalf("%s not in types.yaml", tc.kind)
			}

			var mu sync.Mutex
			var posts []map[string]any
			api := apifake.New(f)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost && r.URL.Path == tc.wantPath {
					b, _ := io.ReadAll(r.Body)
					var m map[string]any
					_ = json.Unmarshal(b, &m)
					mu.Lock()
					posts = append(posts, m)
					mu.Unlock()
					r.Body = io.NopCloser(bytes.NewReader(b))
				}
				api.ServeHTTP(w, r)
			}))
			defer srv.Close()

			spec := map[string]any{tc.parent: "A1", "id": "PREF123", "type": tc.refType}
			reconcileOnce(t, f, k, srv.URL, spec)

			if len(posts) != 1 {
				t.Fatalf("got %d POSTs to %s, want 1", len(posts), tc.wantPath)
			}
			want := map[string]any{tc.envelope: map[string]any{"id": "PREF123", "type": tc.refType}}
			if !reflect.DeepEqual(posts[0], want) {
				t.Fatalf("create body = %v, want %v", posts[0], want)
			}
		})
	}
}

func crdSaysIDRequiredAndTypeDefaults(t *testing.T, plural, refType string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("config", "crds", "automation.pagerduty.hfl.dev_"+plural+".yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var crd apiextv1.CustomResourceDefinition
	if err := yaml.Unmarshal(b, &crd); err != nil {
		t.Fatal(err)
	}
	spec := crd.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties["spec"]
	if _, ok := spec.Properties["id"]; !ok {
		t.Fatalf("CRD spec has no id")
	}
	typ, ok := spec.Properties["type"]
	if !ok {
		t.Fatalf("CRD spec has no type")
	}
	if typ.Default == nil || string(typ.Default.Raw) != `"`+refType+`"` {
		t.Fatalf("CRD spec.type default = %v, want %q", typ.Default, refType)
	}
	// Required: plainly, or by a CEL rule that lets a reference fill it
	// (spec.fieldRefs.id or spec.secretRefs.id).
	req := map[string]bool{}
	for _, r := range spec.Required {
		req[r] = true
	}
	for _, v := range spec.XValidations {
		if strings.HasPrefix(v.Rule, "has(self.id) ||") {
			req["id"] = true
		}
	}
	if !req["id"] {
		t.Fatalf("CRD spec.required = %v and no CEL rule requires id, want id required", spec.Required)
	}
}

func reconcileOnce(t *testing.T, f *descriptor.File, k *descriptor.Kind, baseURL string, spec map[string]any) {
	t.Helper()
	const ns = "team-a"
	gvk := k.GVK()
	mapper := meta.NewDefaultRESTMapper(nil)
	mapper.Add(gvk, meta.RESTScopeNamespace)
	mapper.Add(f.ConnectionGVK(), meta.RESTScopeNamespace)
	mapper.Add(corev1.SchemeGroupVersion.WithKind("Secret"), meta.RESTScopeNamespace)
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "creds"}, Data: map[string][]byte{"token": []byte("t")}}
	conn := &unstructured.Unstructured{}
	conn.SetGroupVersionKind(f.ConnectionGVK())
	conn.SetNamespace(ns)
	conn.SetName("default")
	_ = unstructured.SetNestedField(conn.Object, baseURL, "spec", "baseURL")
	_ = unstructured.SetNestedField(conn.Object, "creds", "spec", "secretRef", "name")

	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gvk)
	obj.SetNamespace(ns)
	obj.SetName("assoc")
	obj.Object["spec"] = spec

	c := fake.NewClientBuilder().WithScheme(scheme).WithRESTMapper(mapper).
		WithObjects(secret, conn, obj).WithStatusSubresource(obj).Build()
	r := &reconciler.Reconciler{Client: c, Kind: k, Vendor: Vendor, Connection: f.ConnectionGVK()}
	req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: ns, Name: "assoc"}}
	if _, err := r.Reconcile(context.Background(), req); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	got := &unstructured.Unstructured{}
	got.SetGroupVersionKind(gvk)
	if err := c.Get(context.Background(), req.NamespacedName, got); err != nil {
		t.Fatal(err)
	}
	conds, _, _ := unstructured.NestedSlice(got.Object, "status", "conditions")
	if len(conds) == 0 || conds[0].(map[string]any)["status"] != "True" {
		t.Fatalf("not Ready: %v", conds)
	}
}
