/*
** Copyright [2013-2015] [Megam Systems]
**
** Licensed under the Apache License, Version 2.0 (the "License");
** you may not use this file except in compliance with the License.
** You may obtain a copy of the License at
**
** http://www.apache.org/licenses/LICENSE-2.0
**
** Unless required by applicable law or agreed to in writing, software
** distributed under the License is distributed on an "AS IS" BASIS,
** WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
** See the License for the specific language governing permissions and
** limitations under the License.
 */

// Package chefsolo implements a provisioner using Chef Solo.
package chefsolo

import (
	"encoding/json"
	"fmt"
	log "github.com/Sirupsen/logrus"
	"github.com/megamsys/gulp/meta"
	"github.com/megamsys/gulp/provision"
	"github.com/megamsys/gulp/repository"
	_ "github.com/megamsys/gulp/repository/github"
	"github.com/megamsys/libgo/action"
	"io"
	"os"
	"path"
	"strings"
)

const (
	// DefaultFormat is the default output format of Chef.
	DefaultFormat = "doc"

	// DefaultLogLevel is the set log level (default: info)
	DefaultLogLevel = "info"

	//set the default sandbox path
	DefaultSandBoxPath = "/var/lib/megam"

	//set the default root path
	DefaultRootPath = "/var/lib/megam"

	//Do not run commands with sudo (enabled by default)
	DefaultSudo = true

	Repository        = "repository"
	RepositoryPath    = "repository_path"
	RepositoryTarPath = "repository_tar_path"
	HomeDir           = "dir"
	RECEIPE           = "receipe"

	SOURCE = "source"
)

var mainChefSoloProvisioner *chefsoloProvisioner

func init() {
	mainChefSoloProvisioner = &chefsoloProvisioner{}
	provision.Register("chefsolo", mainChefSoloProvisioner)
}

type Attributes struct {
	RunList     []string `json:"run_list"`
	ToscaType   string   `json:"tosca_type"`
	RabbitmqURL string   `json:"rabbitmq_url"`
	Scm					string   `json:"scm"`
}

// Provisioner is a provisioner based on Chef Solo.
type chefsoloProvisioner struct {
	RunList     []string
	Attributes  string
	Format      string
	LogLevel    string
	SandboxPath string
	RootPath    string
	Sudo        bool
}

type runRepositoryActionArgs struct {
	repository string
	url        string
	tar_url    string
	dir        string
}

//initialize the provisioner and setup the requirements for provisioner
func (p *chefsoloProvisioner) Initialize(m map[string]string) error {
	//  p.RunList = []string{ m["receipe"] }
	args := &runRepositoryActionArgs{
		repository: m[Repository],
		url:        m[RepositoryPath],
		tar_url:    m[RepositoryTarPath],
		dir:        m[HomeDir],
	}
	return p.setupRequirements(args)
}

//this setup the requirements for provisioner using megam default repository
func (p *chefsoloProvisioner) setupRequirements(args *runRepositoryActionArgs) error {
	a, err := repository.Get(args.repository)

	if err != nil {
		log.Errorf("fatal error, couldn't locate the Repository %s", args.repository)
		return err
	}

	provision.Repository = a

	if initializableRepository, ok := provision.Repository.(repository.InitializableRepository); ok {
		log.Debugf("Before repository initialization.")

		s := strings.Split(args.url, "/")[4]
		filename := strings.Split(s, ".")[0]
		log.Debugf(args.dir + filename)

		_, er := os.Stat(args.dir + "/" + filename)
		if er != nil {
			err = initializableRepository.Initialize(args.url, args.tar_url)
			if err != nil {
				log.Errorf("fatal error, couldn't initialize the Repository %s", args.tar_url)
				return err
			} else {
				log.Debugf("%s Initialized", args.repository)
				return nil
			}
		} else {
			log.Debugf("%s/%s Directory already exist", args.dir, filename)
			return nil
		}
	}
	return nil
}

func (p *chefsoloProvisioner) StartupMessage() (string, error) {
	out := "chefsolo provisioner reports the following:\n"
	out += fmt.Sprintf("    chef-solo provisioner initiated. ")
	return out, nil
}

/* new state */
func (p *chefsoloProvisioner) Deploy(box *provision.Box, w io.Writer) error {
	var repo string

  if box.Repo != nil {
		repo = box.Repo.Url
	}

	res1D := &Attributes{
		RunList:     []string{"recipe[" + box.Cookbook + "]"},
		ToscaType:   strings.Split(box.Tosca, ".")[2],
		RabbitmqURL: meta.MC.AMQP,
		Scm:				 repo,
	}

	DefaultAttributes, _ := json.Marshal(res1D)

	p.Attributes = string(DefaultAttributes)
	p.Format = DefaultFormat
	p.LogLevel = DefaultLogLevel
	p.SandboxPath = DefaultSandBoxPath
	p.RootPath = DefaultRootPath
	p.Sudo = DefaultSudo

	log.Info("Provisioner = %+v\n", p)

	return p.createPipeline(box, w)
}

//1. &prepareJSON in generate the json file for chefsolo
//2. &prepareConfig in generate the config file for chefsolo.
//3. &updateStatus in Riak - Creating..
func (p *chefsoloProvisioner) createPipeline(box *provision.Box, w io.Writer) error {
	actions := []*action.Action{
		&prepareJSON,
		&prepareConfig,
		&prepareBoxRepository,
		&deploy,
		&updateStatusInRiak,
	}
	pipeline := action.NewPipeline(actions...)
	args := runMachineActionsArgs{
		box:           box,
		writer:        w,
		machineStatus: provision.StatusRunning,
		provisioner:   p,
	}

	err := pipeline.Execute(args)
	if err != nil {
		log.Errorf("error on execute create pipeline for box %s - %s", box.GetFullName(), err)
		return err
	}
	return nil
}

// Command returns the command string which will invoke the provisioner on the
// prepared machine.
func (p chefsoloProvisioner) Command() []string {
	format := p.Format
	if format == "" {
		format = DefaultFormat
	}

	logLevel := p.LogLevel
	if logLevel == "" {
		logLevel = DefaultLogLevel
	}

	cmd := []string{
		"chef-solo",
		"--config", path.Join(p.RootPath, "solo.rb"),
		"--json-attributes", path.Join(p.RootPath, "solo.json"),
		"--format", format,
		"--log_level", logLevel,
	}

	//if len(p.RunList) > 0 {
	//	cmd = append(cmd, "--override-runlist", strings.Join(p.RunList, ","))
	//}
	log.Debugf("provisioner command is  %s", cmd)

	if !p.Sudo {
		return cmd
	}
	return append([]string{"sudo"}, cmd...)
}
