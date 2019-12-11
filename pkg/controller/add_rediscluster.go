package controller

import (
	"xzbc-temp/pkg/controller/rediscluster"
)

func init() {
	// AddToManagerFuncs is a list of functions to create controllers and add them to a manager.
	AddToManagerFuncs = append(AddToManagerFuncs, rediscluster.Add)
}
