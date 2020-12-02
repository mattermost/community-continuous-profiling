package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

type uploadResponse struct {
	FileInfos []struct {
		ID     string `json:"id"`
		UserID string `json:"user_id"`
		Name   string `json:"name"`
	} `json:"file_infos"`
}

type environmentVariables struct {
	UploadAPIURL             string
	PostAPIURL               string
	MattermostProfileTargets []string
	ProfilingTime            string
	ChannelID                string
	Token                    string
}

func main() {
	envVars, err := validateAndGetEnvVars()
	if err != nil {
		log.WithError(err).Error("Environment variable validation failed")
	}

	uploads := []string{}
	log.Infof("Running profiling for %s", strings.Join(envVars.MattermostProfileTargets, ", "))
	err = profiling(envVars.ProfilingTime, envVars.MattermostProfileTargets)
	if err != nil {
		log.WithError(err).Error("Failed to run profiling")
	}

	log.Infof("Uploading files in channel - %s", envVars.ChannelID)
	for _, target := range envVars.MattermostProfileTargets {
		memFile := fmt.Sprintf("%s_mem.prof", target)

		memValues := map[string]io.Reader{
			"files":      openFile(memFile),
			"channel_id": strings.NewReader(envVars.ChannelID),
		}
		log.Infof("Uploading file %s", memFile)
		memResponse, err := uploadFile(envVars.UploadAPIURL, envVars.Token, memValues)
		if err != nil {
			log.WithError(err).Errorf("Failed to upload file %s", memFile)
		}
		uploads = append(uploads, memResponse.FileInfos[0].ID)

		cpuFile := fmt.Sprintf("%s_cpu.prof", target)

		cpuValues := map[string]io.Reader{
			"files":      openFile(cpuFile),
			"channel_id": strings.NewReader(envVars.ChannelID),
		}
		log.Infof("Uploading file %s", cpuFile)
		cpuResponse, err := uploadFile(envVars.UploadAPIURL, envVars.Token, cpuValues)
		if err != nil {
			log.WithError(err).Errorf("Failed to upload file %s", cpuFile)
		}
		uploads = append(uploads, cpuResponse.FileInfos[0].ID)
	}

	log.Info("Posting files")
	err = postFile(envVars.PostAPIURL, envVars.ChannelID, envVars.Token, uploads, envVars.MattermostProfileTargets)
	if err != nil {
		log.WithError(err).Error("Failed to post files")
	}

}

// validateEnvironmentVariables is used to validate the environment variables needed by community continuous profiling.
func validateAndGetEnvVars() (*environmentVariables, error) {
	envVars := &environmentVariables{}
	uploadAPIURL := os.Getenv("UPLOAD_API_URL")
	if len(uploadAPIURL) == 0 {
		return nil, errors.Errorf("UPLOAD_API_URL environment variable is not set")
	}
	envVars.UploadAPIURL = uploadAPIURL

	postAPIURL := os.Getenv("POST_API_URL")
	if len(postAPIURL) == 0 {
		return nil, errors.Errorf("POST_API_URL environment variable is not set")
	}
	envVars.PostAPIURL = postAPIURL

	mattermostProfileTargets := os.Getenv("MATTERMOST_PROFILE_TARGETS")
	if len(mattermostProfileTargets) == 0 {
		envVars.MattermostProfileTargets = []string{}
	} else {
		envVars.MattermostProfileTargets = strings.Split(mattermostProfileTargets, ",")
	}

	profilingTime := os.Getenv("PROFILING_TIME")
	if len(profilingTime) == 0 {
		return nil, errors.Errorf("PROFILING_TIME environment variable is not set.")
	}
	envVars.ProfilingTime = profilingTime

	channelID := os.Getenv("CHANNEL_ID")
	if len(channelID) == 0 {
		return nil, errors.Errorf("CHANNEL_ID environment variable is not set.")
	}
	envVars.ChannelID = channelID

	token := os.Getenv("TOKEN")
	if len(token) == 0 {
		return nil, errors.Errorf("TOKEN environment variable is not set.")
	}
	envVars.Token = token

	return envVars, nil
}

func profiling(seconds string, targets []string) (err error) {
	for _, target := range targets {
		log.Infof("Running memory profiling for %s", target)
		memCMD := exec.Command("curl", fmt.Sprintf("http://%s:8067/debug/pprof/heap", target), "-o", fmt.Sprintf("%s_mem.prof", target))
		memCMD.Stdout = os.Stdout
		memCMD.Stderr = os.Stderr
		err = memCMD.Run()
		if err != nil {
			return err
		}

		log.Infof("Running cpu profiling for %s", target)
		cpuCmd := exec.Command("curl", fmt.Sprintf("http://%s:8067/debug/pprof/profile?seconds=%s", target, seconds), "-o", fmt.Sprintf("%s_cpu.prof", target))
		cpuCmd.Stdout = os.Stdout
		cpuCmd.Stderr = os.Stderr
		err = cpuCmd.Run()
		if err != nil {
			return err
		}
	}

	return nil
}

func postFile(url, channelID, token string, files, targets []string) (err error) {
	currentTime := time.Now()
	requestBody, err := json.Marshal(map[string]interface{}{
		"channel_id": channelID,
		"message":    fmt.Sprintf("### CPU and Memory profiles for %s (%s UTC)", strings.Join(targets, ", "), currentTime.Format("2006-01-02 15:04:05")),
		"file_ids":   files,
	},
	)
	var jsonStr = []byte(requestBody)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonStr))
	req.Header.Set("authorization", fmt.Sprintf("Bearer %s", token))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return nil
}

func uploadFile(url, token string, values map[string]io.Reader) (data uploadResponse, err error) {
	data = uploadResponse{}
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	for key, r := range values {
		var fw io.Writer
		if x, ok := r.(io.Closer); ok {
			defer x.Close()
		}
		// Add an image file
		if x, ok := r.(*os.File); ok {
			if fw, err = w.CreateFormFile(key, x.Name()); err != nil {
				return
			}
		} else {
			// Add other fields
			if fw, err = w.CreateFormField(key); err != nil {
				return
			}
		}
		if _, err = io.Copy(fw, r); err != nil {
			return data, err
		}
	}
	w.Close()

	req, err := http.NewRequest("POST", url, &b)
	if err != nil {
		return data, err
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	req.Header.Set("Content-Type", w.FormDataContentType())

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return data, err
	}

	defer resp.Body.Close()

	responseData, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return data, err
	}
	err = json.Unmarshal(responseData, &data)
	if err != nil {
		return data, err
	}

	return data, nil
}

func openFile(f string) *os.File {
	r, err := os.Open(f)
	if err != nil {
		panic(err)
	}
	return r
}
