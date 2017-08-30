// Copyright 2016-2017, Pulumi Corporation.  All rights reserved.

package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/golang/glog"
	"github.com/pulumi/pulumi-fabric/pkg/encoding"
	"github.com/pulumi/pulumi-fabric/pkg/resource/deploy"
	"github.com/pulumi/pulumi-fabric/pkg/resource/environment"
	"github.com/pulumi/pulumi-fabric/pkg/tokens"
	"github.com/pulumi/pulumi-fabric/pkg/util/contract"
	"github.com/pulumi/pulumi-fabric/pkg/util/mapper"
	"github.com/pulumi/pulumi-fabric/pkg/workspace"
)

type fileSystemEnvironmentProvider struct{}

func (p fileSystemEnvironmentProvider) GetEnvironment(name tokens.QName) (*deploy.Target,
	*deploy.Snapshot, *environment.Checkpoint, error) {

	contract.Require(name != "", "name")
	file := workspace.EnvPath(name)

	// Detect the encoding of the file so we can do our initial unmarshaling.
	m, ext := encoding.Detect(file)
	if m == nil {
		glog.Errorf("Resource deserialization failed; illegal markup extension: '%v'", ext)
		return nil, nil, nil, fmt.Errorf("resource deserialization failed; illegal markup extension: '%v'", ext)
	}

	// Now read the whole file into a byte blob.
	b, err := ioutil.ReadFile(file)
	if err != nil {
		if os.IsNotExist(err) {
			glog.Errorf("Environment '%v' could not be found in the current workspace", name)
		} else {
			glog.Errorf("An IO error occurred during the current operation: %v", err)
		}
		return nil, nil, nil, err
	}

	// Unmarshal the contents into a checkpoint structure.
	var checkpoint environment.Checkpoint
	if err = m.Unmarshal(b, &checkpoint); err != nil {
		glog.Errorf("Could not read deployment file '%v': %v", file, err)
		return nil, nil, nil, err
	}

	// Next, use the mapping infrastructure to validate the contents.
	// IDEA: we can eliminate this redundant unmarshaling once Go supports strict unmarshaling.
	var obj map[string]interface{}
	if err = m.Unmarshal(b, &obj); err != nil {
		glog.Errorf("Could not read deployment file '%v': %v", file, err)
		return nil, nil, nil, err
	}

	if obj["latest"] != nil {
		if latest, islatest := obj["latest"].(map[string]interface{}); islatest {
			delete(latest, "resources") // remove the resources, since they require custom marshaling.
		}
	}
	md := mapper.New(nil)
	var ignore environment.Checkpoint // just for errors.
	if err = md.Decode(obj, &ignore); err != nil {
		glog.Errorf("Could not read deployment file '%v': %v", file, err)
		return nil, nil, nil, err
	}

	target, snapshot := environment.DeserializeCheckpoint(&checkpoint)
	contract.Assert(target != nil)
	return target, snapshot, &checkpoint, nil
}

func (p fileSystemEnvironmentProvider) SaveEnvironment(env *deploy.Target, snap *deploy.Snapshot) error {
	contract.Require(env != nil, "env")
	file := workspace.EnvPath(env.Name)

	// Make a serializable LumiGL data structure and then use the encoder to encode it.
	m, ext := encoding.Detect(file)
	if m == nil {
		glog.Errorf("Resource serialization failed; illegal markup extension: '%v'", ext)
		return fmt.Errorf("resource serialization failed; illegal markup extension: '%v'", ext)
	}
	if filepath.Ext(file) == "" {
		file = file + ext
	}
	dep := environment.SerializeCheckpoint(env, snap)
	b, err := m.Marshal(dep)
	if err != nil {
		glog.Errorf("An IO error occurred during the current operation: %v", err)
		return err
	}

	// Back up the existing file if it already exists.
	backupTarget(file)

	// Ensure the directory exists.
	if err = os.MkdirAll(filepath.Dir(file), 0700); err != nil {
		glog.Errorf("An IO error occurred during the current operation: %v", err)
		return err
	}

	// And now write out the new snapshot file, overwriting that location.
	if err = ioutil.WriteFile(file, b, 0600); err != nil {
		glog.Errorf("An IO error occurred during the current operation: %v", err)
		return err
	}

	return nil
}

func (p fileSystemEnvironmentProvider) RemoveEnvironment(env *deploy.Target) error {
	contract.Require(env != nil, "env")
	// Just make a backup of the file and don't write out anything new.
	file := workspace.EnvPath(env.Name)
	backupTarget(file)
	return nil
}

// backupTarget makes a backup of an existing file, in preparation for writing a new one.  Instead of a copy, it
// simply renames the file, which is simpler, more efficient, etc.
func backupTarget(file string) {
	contract.Require(file != "", "file")
	err := os.Rename(file, file+".bak")
	contract.IgnoreError(err) // ignore errors.
	// IDEA: consider multiple backups (.bak.bak.bak...etc).
}
