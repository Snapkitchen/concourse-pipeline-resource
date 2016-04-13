package out

import (
	"crypto/md5"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v2"

	"github.com/robdimsdale/concourse-pipeline-resource/concourse"
	"github.com/robdimsdale/concourse-pipeline-resource/concourse/api"
	"github.com/robdimsdale/concourse-pipeline-resource/fly"
	"github.com/robdimsdale/concourse-pipeline-resource/logger"
	"github.com/robdimsdale/concourse-pipeline-resource/pipelinerunner"
)

const (
	apiPrefix = "/api/v1"
)

type OutCommand struct {
	logger        logger.Logger
	binaryVersion string
	flyConn       fly.FlyConn
	apiClient     api.Client
	sourcesDir    string
}

func NewOutCommand(
	binaryVersion string,
	logger logger.Logger,
	flyConn fly.FlyConn,
	apiClient api.Client,
	sourcesDir string,
) *OutCommand {
	return &OutCommand{
		logger:        logger,
		binaryVersion: binaryVersion,
		flyConn:       flyConn,
		apiClient:     apiClient,
		sourcesDir:    sourcesDir,
	}
}

func (c *OutCommand) Run(input concourse.OutRequest) (concourse.OutResponse, error) {
	c.logger.Debugf("Received input: %+v\n", input)

	c.logger.Debugf("Performing login\n")

	_, err := c.flyConn.Login(
		input.Source.Target,
		input.Source.Username,
		input.Source.Password,
	)
	if err != nil {
		return concourse.OutResponse{}, err
	}

	c.logger.Debugf("Login successful\n")

	c.logger.Debugf("Getting pipelines\n")
	var pipelines []concourse.Pipeline
	if input.Params.PipelinesFile != "" {
		b, err := ioutil.ReadFile(filepath.Join(c.sourcesDir, input.Params.PipelinesFile))
		if err != nil {
			return concourse.OutResponse{}, err
		}

		var fileContents concourse.OutParams
		err = yaml.Unmarshal(b, &fileContents)
		if err != nil {
			return concourse.OutResponse{}, err
		}

		pipelines = fileContents.Pipelines
	} else {
		pipelines = input.Params.Pipelines
	}

	for _, p := range pipelines {
		configFilepath := filepath.Join(c.sourcesDir, p.ConfigFile)

		var varsFilepaths []string
		for _, v := range p.VarsFiles {
			varFilepath := filepath.Join(c.sourcesDir, v)
			varsFilepaths = append(varsFilepaths, varFilepath)
		}

		_, err := c.flyConn.SetPipeline(p.Name, configFilepath, varsFilepaths)
		if err != nil {
			return concourse.OutResponse{}, err
		}
	}

	apiPipelines, err := c.apiClient.Pipelines()
	if err != nil {
		return concourse.OutResponse{}, err
	}

	c.logger.Debugf("Found pipelines: %+v\n", apiPipelines)

	gpFunc := func(index int, pipeline api.Pipeline) (string, error) {
		c.logger.Debugf("Getting pipeline: %s\n", pipeline.Name)
		outBytes, err := c.flyConn.GetPipeline(pipeline.Name)

		c.logger.Debugf("%s stdout: %s\n",
			pipeline.Name,
			string(outBytes),
		)

		if err != nil {
			return "", err
		}

		return string(outBytes), nil
	}

	pipelinesContents, err := pipelinerunner.RunForAllPipelines(gpFunc, apiPipelines, c.logger)
	if err != nil {
		return concourse.OutResponse{}, err
	}

	allContent := strings.Join(pipelinesContents, "")

	pipelinesChecksumString := fmt.Sprintf(
		"%x",
		md5.Sum([]byte(allContent)),
	)
	c.logger.Debugf("pipeline content checksum: %s\n", pipelinesChecksumString)

	metadata := []concourse.Metadata{}

	response := concourse.OutResponse{
		Version: concourse.Version{
			PipelinesChecksum: pipelinesChecksumString,
		},
		Metadata: metadata,
	}

	return response, nil
}
