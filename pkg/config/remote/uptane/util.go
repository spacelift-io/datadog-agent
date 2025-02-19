// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-present Datadog, Inc.

package uptane

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path"
	"strconv"
	"strings"
)

type metaPath struct {
	role       role
	version    uint64
	versionSet bool
}

func parseMetaPath(rawMetaPath string) (metaPath, error) {
	splitRawMetaPath := strings.SplitN(rawMetaPath, ".", 3)
	if len(splitRawMetaPath) != 2 && len(splitRawMetaPath) != 3 {
		return metaPath{}, fmt.Errorf("invalid metadata path '%s'", rawMetaPath)
	}
	suffix := splitRawMetaPath[len(splitRawMetaPath)-1]
	if suffix != "json" {
		return metaPath{}, fmt.Errorf("invalid metadata path (suffix) '%s'", rawMetaPath)
	}
	rawRole := splitRawMetaPath[len(splitRawMetaPath)-2]
	if rawRole == "" {
		return metaPath{}, fmt.Errorf("invalid metadata path (role) '%s'", rawMetaPath)
	}
	if len(splitRawMetaPath) == 2 {
		return metaPath{
			role: role(rawRole),
		}, nil
	}
	rawVersion, err := strconv.ParseUint(splitRawMetaPath[0], 10, 64)
	if err != nil {
		return metaPath{}, fmt.Errorf("invalid metadata path (version) '%s': %w", rawMetaPath, err)
	}
	return metaPath{
		role:       role(rawRole),
		version:    rawVersion,
		versionSet: true,
	}, nil
}

func metaVersion(rawMeta json.RawMessage) (uint64, error) {
	var metaVersion struct {
		Signed *struct {
			Version *uint64 `json:"version"`
		} `json:"signed"`
	}
	err := json.Unmarshal(rawMeta, &metaVersion)
	if err != nil {
		return 0, err
	}
	if metaVersion.Signed == nil || metaVersion.Signed.Version == nil {
		return 0, fmt.Errorf("invalid meta: version field is missing")
	}
	return *metaVersion.Signed.Version, nil
}

func trimHashTargetPath(targetPath string) string {
	basename := path.Base(targetPath)
	split := strings.SplitN(basename, ".", 2)
	if len(split) > 1 {
		basename = split[1]
	}
	return path.Join(path.Dir(targetPath), basename)
}

type bufferDestination struct {
	bytes.Buffer
}

func (b *bufferDestination) Delete() error {
	return nil
}
