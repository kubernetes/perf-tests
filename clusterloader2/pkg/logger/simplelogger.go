/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package logger

import (
	"log"
	"os"
)

type simpleLogger struct {
	logDirPath string
	logger     *log.Logger
}

func createSimpleLogger(logDirPath string) Interface {
	return &simpleLogger{
		logDirPath: logDirPath,
		logger:     log.New(os.Stdout, "", log.Lshortfile),
	}
}

func (sl *simpleLogger) Infof(format string, args ...interface{}) {
	sl.logger.Printf("INFO: "+format, args...)
}

func (sl *simpleLogger) Errorf(format string, args ...interface{}) {
	sl.logger.Printf("ERROR: "+format, args...)
}

func (sl *simpleLogger) InfofToFile(fileName, format string, args ...interface{}) {
	// TODO(krzysied): Logging to files.
	sl.logger.Printf(fileName+": INFO: "+format, args...)
}

func (sl *simpleLogger) ErrorfToFile(fileName, format string, args ...interface{}) {
	// TODO(krzysied): Logging to files.
	sl.logger.Printf(fileName+": ERROR: "+format, args...)
}
