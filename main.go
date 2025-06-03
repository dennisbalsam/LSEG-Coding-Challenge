package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// endpoints and data fetching used as specified here: https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/configuring-instance-metadata-service.html
const (
	tokenUrl    = "http://169.254.169.254/latest/api/token"
	metadataUrl = "http://169.254.169.254/latest/meta-data"
)

var (
	// reuse client
	client = &http.Client{}
)

// gets token from tokenUrl
func fetchToken() (string, error) {
	req, err := http.NewRequest("PUT", tokenUrl, nil)
	if err != nil {
		return "", err
	}
	// token lifetime
	req.Header.Set("X-aws-ec2-metadata-token-ttl-seconds", "21600")

	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != 200 {
		return "", err
	}
	defer resp.Body.Close()

	token, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return string(token), nil
}

// gets metadata from specified path
func getMetadata(token, path string) any {
	req, _ := http.NewRequest("GET", fmt.Sprintf("%s/%s", metadataUrl, path), nil)

	// active token
	req.Header.Set("X-aws-ec2-metadata-token", token)

	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != 200 {
		return nil
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	content := string(body)

	// if returned metadata is name of another directory, recursively get data: https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/ec2-instance-metadata.html#instancedata-data-categories
	if strings.HasSuffix(content, "/") || strings.Contains(content, "\n") && !isJsonObject(content) && !isCertOrKey(content) {
		result := map[string]any{}
		lines := strings.Split(content, "\n")
		for _, line := range lines {
			if line != "" {
				result[strings.TrimSuffix(line, "/")] = getMetadata(token, path+line)
			}
		}
		return result
	}

	// if returned metadata looks like pure json object, unmarshal json string into any and return
	if isJsonObject(content) {
		var jsonObj any
		err := json.Unmarshal([]byte(content), &jsonObj)

		if err != nil {
			return nil
		}
		return jsonObj
	}

	// if returned metadata looks like certs and keys return as is
	return content
}

// some of the fields returned were pure json object, so need to preserve its structure, example: /identity-credentials/ec2/info
func isJsonObject(s string) bool {
	return (strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}")) || (strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]"))
}

// some of the fields returned were certs and keys, so better to keep as string, example: /managed-ssh-keys/signer-cert
func isCertOrKey(s string) bool {
	return strings.Contains(s, "-----BEGIN") || strings.Contains(s, "-----END")
}

func main() {
	token, err := fetchToken()
	if err != nil {
		fmt.Println("error getting token:", err)
		os.Exit(1)
	}

	key := ""
	args := os.Args
	if len(args) > 1 {
		key = args[1]
	}

	var metadata any
	if key != "" {
		metadata = map[string]any{
			key: getMetadata(token, key),
		}
	} else {
		metadata = getMetadata(token, "")
	}

	b, err := json.MarshalIndent(metadata, "", "\t")
	if err != nil {
		fmt.Println("error structuring json output:", err)
		os.Exit(1)
	}

	// write to output file
	err = os.WriteFile("output.json", b, 0644)
	if err != nil {
		fmt.Println("error writing to output file:", err)
		os.Exit(1)
	}

}
