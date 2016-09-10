package in

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/concourse/atc"
	"github.com/robdimsdale/concourse-pipeline-resource/concourse"
	"github.com/robdimsdale/concourse-pipeline-resource/concourse/api"
)

//go:generate counterfeiter . Client
type Client interface {
	Pipelines(teamName string) ([]api.Pipeline, error)
	PipelineConfig(teamName string, pipelineName string) (config atc.Config, rawConfig string, version string, err error)
}

//go:generate counterfeiter . Logger
type Logger interface {
	Debugf(format string, a ...interface{}) (n int, err error)
}

type InCommand struct {
	logger        Logger
	binaryVersion string
	apiClient     Client
	downloadDir   string
}

func NewInCommand(
	binaryVersion string,
	logger Logger,
	apiClient Client,
	downloadDir string,
) *InCommand {
	return &InCommand{
		logger:        logger,
		binaryVersion: binaryVersion,
		apiClient:     apiClient,
		downloadDir:   downloadDir,
	}
}

func (c *InCommand) Run(input concourse.InRequest) (concourse.InResponse, error) {
	c.logger.Debugf("Received input: %+v\n", input)

	c.logger.Debugf("Creating download directory: %s\n", c.downloadDir)
	err := os.MkdirAll(c.downloadDir, os.ModePerm)
	if err != nil {
		log.Fatalf("Failed to create download directory: %s\n", err.Error())
	}

	c.logger.Debugf("Getting pipelines\n")

	teamName := input.Source.Teams[0].Name
	pipelines, err := c.apiClient.Pipelines(teamName)
	if err != nil {
		return concourse.InResponse{}, err
	}

	c.logger.Debugf("Found pipelines: %+v\n", pipelines)

	var wg sync.WaitGroup
	wg.Add(len(pipelines))

	errChan := make(chan error, len(pipelines))

	pipelinesWithContents := make([]pipelineWithContent, len(pipelines))
	for i, p := range pipelines {
		go func(i int, p api.Pipeline) {
			defer wg.Done()

			_, config, _, err := c.apiClient.PipelineConfig(teamName, p.Name)
			if err != nil {
				errChan <- err
			}
			pipelinesWithContents[i] = pipelineWithContent{
				name:     p.Name,
				contents: config,
			}
		}(i, p)
	}

	c.logger.Debugf("Waiting for all pipelines\n")
	wg.Wait()
	c.logger.Debugf("Waiting for all pipelines complete\n")

	close(errChan)
	for err := range errChan {
		if err != nil {
			return concourse.InResponse{}, err
		}
	}

	for _, p := range pipelinesWithContents {
		pipelineContentsFilepath := filepath.Join(c.downloadDir, fmt.Sprintf("%s.yml", p.name))
		c.logger.Debugf("Writing pipeline contents to: %s\n", pipelineContentsFilepath)
		err = ioutil.WriteFile(pipelineContentsFilepath, []byte(p.contents), os.ModePerm)
		if err != nil {
			// Untested as it is too hard to force ioutil.WriteFile to error
			return concourse.InResponse{}, err
		}
	}

	response := concourse.InResponse{
		Version:  input.Version,
		Metadata: []concourse.Metadata{},
	}

	return response, nil
}

type pipelineWithContent struct {
	name     string
	contents string
}
