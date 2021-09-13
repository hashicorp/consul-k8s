package test

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/hashicorp/consul-k8s/control-plane/helper/cert"
	"github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
)

// GenerateServerCerts generates Consul CA
// and a server certificate and saves them to temp files.
// It returns file names in this order:
// CA certificate, server certificate, and server key.
func GenerateServerCerts(t *testing.T) (string, string, string) {
	require := require.New(t)

	caFile, err := ioutil.TempFile("", "ca")
	require.NoError(err)

	certFile, err := ioutil.TempFile("", "cert")
	require.NoError(err)

	certKeyFile, err := ioutil.TempFile("", "key")
	require.NoError(err)

	// Generate CA
	signer, _, caCertPem, caCertTemplate, err := cert.GenerateCA("Consul Agent CA - Test")
	require.NoError(err)

	// Generate Server Cert
	name := "server.dc1.consul"
	hosts := []string{name, "localhost", "127.0.0.1"}
	certPem, keyPem, err := cert.GenerateCert(name, 1*time.Hour, caCertTemplate, signer, hosts)
	require.NoError(err)

	// Write certs and key to files
	_, err = caFile.WriteString(caCertPem)
	require.NoError(err)
	_, err = certFile.WriteString(certPem)
	require.NoError(err)
	_, err = certKeyFile.WriteString(keyPem)
	require.NoError(err)

	t.Cleanup(func() {
		os.Remove(caFile.Name())
		os.Remove(certFile.Name())
		os.Remove(certKeyFile.Name())
	})
	return caFile.Name(), certFile.Name(), certKeyFile.Name()
}

// SetupK8sAuthMethod create a k8s auth method and a binding rule in Consul for the
// given k8s service and namespace.
func SetupK8sAuthMethod(t *testing.T, consulClient *api.Client, serviceName, k8sServiceNS string) {
	SetupK8sAuthMethodWithNamespaces(t, consulClient, serviceName, k8sServiceNS, "", false, "")
}

// SetupK8sAuthMethodWithNamespaces creates a k8s auth method and binding rule
// in Consul for the k8s service name and namespace. It sets up the auth method and the binding
// rule so that it works with consul namespaces.
func SetupK8sAuthMethodWithNamespaces(t *testing.T, consulClient *api.Client, serviceName, k8sServiceNS, consulNS string, mirrorNS bool, nsPrefix string) {
	t.Helper()
	// Start the mock k8s server.
	k8sMockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		if r != nil && r.URL.Path == "/apis/authentication.k8s.io/v1/tokenreviews" && r.Method == "POST" {
			w.Write([]byte(tokenReviewsResponse(serviceName, k8sServiceNS)))
		}
		if r != nil && r.URL.Path == fmt.Sprintf("/api/v1/namespaces/%s/serviceaccounts/%s", k8sServiceNS, serviceName) &&
			r.Method == "GET" {
			w.Write([]byte(serviceAccountGetResponse(serviceName, k8sServiceNS)))
		}
	}))
	t.Cleanup(k8sMockServer.Close)

	// Set up Consul's auth method.
	authMethodTmpl := api.ACLAuthMethod{
		Name:        AuthMethod,
		Type:        "kubernetes",
		Description: "Kubernetes Auth Method",
		Config: map[string]interface{}{
			"Host":              k8sMockServer.URL,
			"CACert":            serviceAccountCACert,
			"ServiceAccountJWT": ServiceAccountJWTToken,
		},
		Namespace: consulNS,
	}
	if mirrorNS {
		authMethodTmpl.Namespace = "default"
		authMethodTmpl.Config["MapNamespaces"] = strconv.FormatBool(mirrorNS)
		authMethodTmpl.Config["ConsulNamespacePrefix"] = nsPrefix
	}
	// This API call will idempotently create the auth method (it won't fail if it already exists).
	_, _, err := consulClient.ACL().AuthMethodCreate(&authMethodTmpl, nil)
	require.NoError(t, err)

	// Create the binding rule.
	aclBindingRule := api.ACLBindingRule{
		Description: "Kubernetes binding rule",
		AuthMethod:  AuthMethod,
		BindType:    api.BindingRuleBindTypeService,
		BindName:    "${serviceaccount.name}",
		Selector:    "serviceaccount.name!=default",
		Namespace:   consulNS,
	}
	if mirrorNS {
		aclBindingRule.Namespace = "default"
	}
	// This API call will idempotently create the binding rule (it won't fail if it already exists).
	_, _, err = consulClient.ACL().BindingRuleCreate(&aclBindingRule, nil)
	require.NoError(t, err)
}

func tokenReviewsResponse(name, ns string) string {
	return fmt.Sprintf(`{
 "kind": "TokenReview",
 "apiVersion": "authentication.k8s.io/v1",
 "metadata": {
   "creationTimestamp": null
 },
 "spec": {
   "token": "eyJhbGciOiJSUzI1NiIsImtpZCI6IiJ9.eyJpc3MiOiJrdWJlcm5ldGVzL3NlcnZpY2VhY2NvdW50Iiwia3ViZXJuZXRlcy5pby9zZXJ2aWNlYWNjb3VudC9uYW1lc3BhY2UiOiJkZWZhdWx0Iiwia3ViZXJuZXRlcy5pby9zZXJ2aWNlYWNjb3VudC9zZWNyZXQubmFtZSI6ImRlbW8tdG9rZW4tbTljdm4iLCJrdWJlcm5ldGVzLmlvL3NlcnZpY2VhY2NvdW50L3NlcnZpY2UtYWNjb3VudC5uYW1lIjoiZGVtbyIsImt1YmVybmV0ZXMuaW8vc2VydmljZWFjY291bnQvc2VydmljZS1hY2NvdW50LnVpZCI6IjlmZjUxZmY0LTU1N2UtMTFlOS05Njg3LTQ4ZTZjOGI4ZWNiNSIsInN1YiI6InN5c3RlbTpzZXJ2aWNlYWNjb3VudDpkZWZhdWx0OmRlbW8ifQ.UJEphtrN261gy9WCl4ZKjm2PRDLDkc3Xg9VcDGfzyroOqFQ6sog5dVAb9voc5Nc0-H5b1yGwxDViEMucwKvZpA5pi7VEx_OskK-KTWXSmafM0Xg_AvzpU9Ed5TSRno-OhXaAraxdjXoC4myh1ay2DMeHUusJg_ibqcYJrWx-6MO1bH_ObORtAKhoST_8fzkqNAlZmsQ87FinQvYN5mzDXYukl-eeRdBgQUBkWvEb-Ju6cc0-QE4sUQ4IH_fs0fUyX_xc0om0SZGWLP909FTz4V8LxV8kr6L7irxROiS1jn3Fvyc9ur1PamVf3JOPPrOyfmKbaGRiWJM32b3buQw7cg"
 },
 "status": {
   "authenticated": true,
   "user": {
	 "username": "system:serviceaccount:%s:%s",
	 "uid": "9ff51ff4-557e-11e9-9687-48e6c8b8ecb5",
	 "groups": [
	   "system:serviceaccounts",
	   "system:serviceaccounts:%s",
	   "system:authenticated"
	 ]
   }
 }
}`, ns, name, ns)
}

func serviceAccountGetResponse(name, ns string) string {
	return fmt.Sprintf(`{
 "kind": "ServiceAccount",
 "apiVersion": "v1",
 "metadata": {
   "name": "%s",
   "namespace": "%s",
   "selfLink": "/api/v1/namespaces/%s/serviceaccounts/%s",
   "uid": "9ff51ff4-557e-11e9-9687-48e6c8b8ecb5",
   "resourceVersion": "2101",
   "creationTimestamp": "2019-04-02T19:36:34Z"
 },
 "secrets": [
   {
	 "name": "%s-token-m9cvn"
   }
 ]
}`, name, ns, ns, name, name)
}

const AuthMethod = "consul-k8s-auth-method"
const ServiceAccountJWTToken = `eyJhbGciOiJSUzI1NiIsImtpZCI6IiJ9.eyJpc3MiOiJrdWJlcm5ldGVzL3NlcnZpY2VhY2NvdW50Iiwia3ViZXJuZXRlcy5pby9zZXJ2aWNlYWNjb3VudC9uYW1lc3BhY2UiOiJkZWZhdWx0Iiwia3ViZXJuZXRlcy5pby9zZXJ2aWNlYWNjb3VudC9zZWNyZXQubmFtZSI6ImtoYWtpLWFyYWNobmlkLWNvbnN1bC1jb25uZWN0LWluamVjdG9yLWF1dGhtZXRob2Qtc3ZjLWFjY29obmRidiIsImt1YmVybmV0ZXMuaW8vc2VydmljZWFjY291bnQvc2VydmljZS1hY2NvdW50Lm5hbWUiOiJraGFraS1hcmFjaG5pZC1jb25zdWwtY29ubmVjdC1pbmplY3Rvci1hdXRobWV0aG9kLXN2Yy1hY2NvdW50Iiwia3ViZXJuZXRlcy5pby9zZXJ2aWNlYWNjb3VudC9zZXJ2aWNlLWFjY291bnQudWlkIjoiN2U5NWUxMjktZTQ3My0xMWU5LThmYWEtNDIwMTBhODAwMTIyIiwic3ViIjoic3lzdGVtOnNlcnZpY2VhY2NvdW50OmRlZmF1bHQ6a2hha2ktYXJhY2huaWQtY29uc3VsLWNvbm5lY3QtaW5qZWN0b3ItYXV0aG1ldGhvZC1zdmMtYWNjb3VudCJ9.Yi63MMtzh5MBWKKd3a7dzCJjTITE15ikFy_Tnpdk_AwdwA9J4AMSGEeHN5vWtCuuFjo_lMJqBBPHkK2AqbnoFUj9m5CopWyqICJQlvEOP4fUQ-Rc0W1P_JjU1rZERHG39b5TMLgKPQguyhaiZEJ6CjVtm9wUTagrgiuqYV2iUqLuF6SYNm6SrKtkPS-lqIO-u7C06wVk5m5uqwIVQNpZSIC_5Ls5aLmyZU3nHvH-V7E3HmBhVyZAB76jgKB0TyVX1IOskt9PDFarNtU3suZyCjvqC-UJA6sYeySe4dBNKsKlSZ6YuxUUmn1Rgv32YMdImnsWg8khf-zJvqgWk7B5EA`
const serviceAccountCACert = `-----BEGIN CERTIFICATE-----
MIIDCzCCAfOgAwIBAgIQKzs7Njl9Hs6Xc8EXou25hzANBgkqhkiG9w0BAQsFADAv
MS0wKwYDVQQDEyQ1OWU2ZGM0MS0yMDhmLTQwOTUtYTI4OS0xZmM3MDBhYzFjYzgw
HhcNMTkwNjA3MTAxNzMxWhcNMjQwNjA1MTExNzMxWjAvMS0wKwYDVQQDEyQ1OWU2
ZGM0MS0yMDhmLTQwOTUtYTI4OS0xZmM3MDBhYzFjYzgwggEiMA0GCSqGSIb3DQEB
AQUAA4IBDwAwggEKAoIBAQDZjHzwqofzTpGpc0MdICS7euvfujUKE3PC/apfDAgB
4jzEFKA78/9+KUGw/c/0SHeSQhN+a8gwlHRnAz1NJcfOIXy4dweUuOkAiFxH8pht
ECwkeNO7z8DoV8ceminCRHGjaRmoMxpZ7g2pZAJNZePxi3y1aNkFAXe9gSUSdjRZ
RXYka7wh2AO9k2dlGFAYB+t3vWwJ6twjG0TtKQrhYM9Od1/oN0E01LzBcZuxkN1k
8gfIHy7bOFCBM2WTEDW/0aAvcAPrO8DLqDJ+6Mjc3r7+zlzl8aQspb0S08pVzki5
Dz//83kyu0phJuij5eB88V7UfPXxXF/EtV6fvrL7MN4fAgMBAAGjIzAhMA4GA1Ud
DwEB/wQEAwICBDAPBgNVHRMBAf8EBTADAQH/MA0GCSqGSIb3DQEBCwUAA4IBAQBv
QsaG6qlcaRktJ0zGhxxJ52NnRV2GcIYPeN3Zv2VXe3ML3Vd6G32PV7lIOhjx3KmA
/uMh6NhqBzsekkTz0PuC3wJyM2OGonVQisFlqx9sFQ3fU2mIGXCa3wC8e/qP8BHS
w7/VeA7lzmj3TQRE/W0U0ZGeoAxn9b6JtT0iMucYvP0hXKTPBWlnzIijamU50r2Y
7ia065Ug2xUN5FLX/vxOA3y4rjpkjWoVQcu1p8TZrVoM3dsGFWp10fDMRiAHTvOH
Z23jGuk6rn9DUHC2xPj3wCTmd8SGEJoV31noJV5dVeQ90wusXz3vTG7ficKnvHFS
xtr5PSwH1DusYfVaGH2O
-----END CERTIFICATE-----`
