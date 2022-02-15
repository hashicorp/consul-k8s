package helpers

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
)

const gitHubAPI = "https://api.github.com"

// FetchLatestConsulVersion uses the GitHub API to fetch the latest version of Consul which was released.
func FetchLatestConsulVersion() (string, error) {
	resp, err := fetch(gitHubAPI + "/repos/hashicorp/consul/releases/latest")
	if err != nil {
		return "", err
	}

	var jsonResp map[string]interface{}
	if err := json.Unmarshal(resp, &jsonResp); err != nil {
		return "", err
	}

	return jsonResp["tag_name"].(string), nil
}

// FetchLatestConsulVersion uses the GitHub API to fetch the n-1 of the latest version of Consul which was released.
func FetchPreviousConsulVersion() (string, error) {
	resp, err := fetch(gitHubAPI + "/repos/hashicorp/consul/releases")
	if err != nil {
		return "", err
	}

	var jsonResp []map[string]interface{}
	if err := json.Unmarshal(resp, &jsonResp); err != nil {
		return "", err
	}

	return jsonResp[1]["tag_name"].(string), nil
}

// FetchLatestControlPlaneVersion uses the GitHub API to fetch the latest version of Consul on Kubernetes which was released.
func FetchLatestControlPlaneVersion() (string, error) {
	resp, err := fetch(gitHubAPI + "/repos/hashicorp/consul-k8s/releases/latest")
	if err != nil {
		return "", err
	}

	var jsonResp map[string]interface{}
	if err := json.Unmarshal(resp, &jsonResp); err != nil {
		return "", err
	}

	return jsonResp["tag_name"].(string), nil
}

// FetchLatestControlPlaneVersion uses the GitHub API to fetch the n-1 of the latest version of Consul on Kubernetes which was released.
func FetchPreviousControlPlaneVersion() (string, error) {
	resp, err := fetch(gitHubAPI + "/repos/hashicorp/consul-k8s/releases")
	if err != nil {
		return "", err
	}

	var jsonResp []map[string]interface{}
	if err := json.Unmarshal(resp, &jsonResp); err != nil {
		return "", err
	}

	return jsonResp[1]["tag_name"].(string), nil
}

// fetch issues a GET request against the given URL and returns the response as a slice of bytes.
func fetch(url string) ([]byte, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if err = resp.Body.Close(); err != nil {
		return nil, err
	}

	return body, err
}
