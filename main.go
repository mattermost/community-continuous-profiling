package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Pod is used to store each deployments pod information
type Pod struct {
	PodName string `json:"podName"`
	PodIP   string `json:"podIP"`
}

type uploadResponse struct {
	FileInfos []struct {
		ID     string `json:"id"`
		UserID string `json:"user_id"`
		Name   string `json:"name"`
	} `json:"file_infos"`
}

type environmentVariables struct {
	UploadAPIURL          string
	PostAPIURL            string
	MattermostDeployments []string
	MattermostNamespace   string
	ProfilingTime         string
	ChannelID             string
	Token                 string
	DevMode               string
}

func main() {
	envVars, err := validateAndGetEnvVars()
	if err != nil {
		log.WithError(err).Error("Environment variable validation failed")
		return
	}

	clientset, err := getClientSet(envVars)
	if err != nil {
		log.WithError(err).Error("Unable to create k8s clientset")
		return
	}

	for _, deployment := range envVars.MattermostDeployments {
		pods := []Pod{}
		deploymentPods, err := getPodsFromDeployment(envVars.MattermostNamespace, deployment, clientset)
		if err != nil {
			log.WithError(err).Error("Unable to get pods from deployment")
			return
		}
		for _, deploymentPod := range deploymentPods.Items {
			var pod Pod
			pod.PodName = deploymentPod.GetName()
			pod.PodIP = deploymentPod.Status.PodIP
			pods = append(pods, pod)
		}

		log.Infof("Running profiling for %s", deployment)
		err = profiling(envVars.ProfilingTime, pods)
		if err != nil {
			log.WithError(err).Error("Failed to run profiling")
			return
		}
		err = uploadPostFiles(pods, *envVars, deployment)
		if err != nil {
			log.WithError(err).Error("Failed to upload and post files")
			return
		}
	}
}

func uploadPostFiles(pods []Pod, envVars environmentVariables, deployment string) error {
	log.Infof("Uploading files in channel - %s", envVars.ChannelID)
	uploads := []string{}
	for _, pod := range pods {
		memFile := fmt.Sprintf("%s_mem.prof", pod.PodName)

		memValues := map[string]io.Reader{
			"files":      openFile(memFile),
			"channel_id": strings.NewReader(envVars.ChannelID),
		}
		log.Infof("Uploading file %s", memFile)
		memResponse, err := uploadFile(envVars.UploadAPIURL, envVars.Token, memValues)
		if err != nil {
			return errors.Errorf("Failed to upload file %s", memFile)
		}
		uploads = append(uploads, memResponse.FileInfos[0].ID)

		cpuFile := fmt.Sprintf("%s_cpu.prof", pod.PodName)

		cpuValues := map[string]io.Reader{
			"files":      openFile(cpuFile),
			"channel_id": strings.NewReader(envVars.ChannelID),
		}
		log.Infof("Uploading file %s", cpuFile)
		cpuResponse, err := uploadFile(envVars.UploadAPIURL, envVars.Token, cpuValues)
		if err != nil {
			return errors.Errorf("Failed to upload file %s", cpuFile)
		}
		uploads = append(uploads, cpuResponse.FileInfos[0].ID)
	}
	log.Info("Posting files")
	err := postFile(envVars.PostAPIURL, envVars.ChannelID, envVars.Token, deployment, uploads)
	if err != nil {
		return errors.Errorf("Failed to post files")
	}
	return nil
}

// GetPodsFromDeployment gets the pods that belong to a given deployment.
func getPodsFromDeployment(namespace, deploymentName string, clientset *kubernetes.Clientset) (*corev1.PodList, error) {
	ctx := context.TODO()
	deployment, err := clientset.AppsV1().Deployments(namespace).Get(ctx, deploymentName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	set := labels.Set(deployment.GetLabels())
	listOptions := metav1.ListOptions{LabelSelector: set.AsSelector().String()}

	return clientset.CoreV1().Pods(namespace).List(ctx, listOptions)
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

	mattermostDeployments := os.Getenv("MATTERMOST_DEPLOYMENTS")
	if len(mattermostDeployments) == 0 {
		envVars.MattermostDeployments = []string{}
	} else {
		envVars.MattermostDeployments = strings.Split(mattermostDeployments, ",")
	}

	mattermostNamespace := os.Getenv("MATTERMOST_NAMESPACE")
	if len(mattermostNamespace) == 0 {
		return nil, errors.Errorf("CHANNEL_ID environment variable is not set.")
	}
	envVars.MattermostNamespace = mattermostNamespace

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

	developerMode := os.Getenv("DEVELOPER_MODE")
	if len(developerMode) == 0 {
		envVars.DevMode = "false"
	} else {
		envVars.DevMode = developerMode
	}

	return envVars, nil
}

func profiling(seconds string, pods []Pod) (err error) {
	for _, pod := range pods {
		log.Infof("Running memory profiling for %s", pod.PodName)
		memoryFileCMD := exec.Command("touch", fmt.Sprintf("%s_mem.prof", pod.PodName))
		memoryFileCMD.Stdout = os.Stdout
		memoryFileCMD.Stderr = os.Stderr
		err = memoryFileCMD.Run()
		if err != nil {
			return err
		}
		memCMD := exec.Command("curl", fmt.Sprintf("http://%s:8067/debug/pprof/heap", pod.PodIP), "-o", fmt.Sprintf("%s_mem.prof", pod.PodName))
		memCMD.Stdout = os.Stdout
		memCMD.Stderr = os.Stderr
		err = memCMD.Run()
		if err != nil {
			return err
		}

		log.Infof("Running cpu profiling for %s", pod.PodName)
		cpuFileCMD := exec.Command("touch", fmt.Sprintf("%s_cpu.prof", pod.PodName))
		cpuFileCMD.Stdout = os.Stdout
		cpuFileCMD.Stderr = os.Stderr
		err = cpuFileCMD.Run()
		if err != nil {
			return err
		}
		cpuCmd := exec.Command("curl", fmt.Sprintf("http://%s:8067/debug/pprof/profile?seconds=%s", pod.PodIP, seconds), "-o", fmt.Sprintf("%s_cpu.prof", pod.PodName))
		cpuCmd.Stdout = os.Stdout
		cpuCmd.Stderr = os.Stderr
		err = cpuCmd.Run()
		if err != nil {
			return err
		}
	}

	return nil
}

func postFile(url, channelID, token, deployment string, files []string) (err error) {
	currentTime := time.Now()
	requestBody, err := json.Marshal(map[string]interface{}{
		"channel_id": channelID,
		"message":    fmt.Sprintf("### CPU and Memory profiles for %s (%s UTC)", deployment, currentTime.Format("2006-01-02 15:04:05")),
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

func getClientSet(envVars *environmentVariables) (*kubernetes.Clientset, error) {
	if envVars.DevMode == "true" {

		kubeconfig := filepath.Join(
			os.Getenv("HOME"), ".kube", "config",
		)

		config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, err
		}
		clientset, err := kubernetes.NewForConfig(config)
		if err != nil {
			return nil, err
		}
		return clientset, nil
	}

	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	return clientset, nil
}
