package meshmodel

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/layer5io/meshery/server/helpers/utils"
	"github.com/layer5io/meshery/server/models"
	"github.com/layer5io/meshkit/logger"
	"github.com/layer5io/meshkit/models/meshmodel"
	"github.com/layer5io/meshkit/models/meshmodel/core/v1alpha1"
	"github.com/pkg/errors"
)

const ArtifactHubComponentsHandler = "kubernetes" //The components generated in output directory will be handled by kubernetes

type EntityRegistrationHelper struct {
	handlerConfig    *models.HandlerConfig
	regManager       *meshmodel.RegistryManager
	componentChan    chan v1alpha1.ComponentDefinition
	relationshipChan chan v1alpha1.RelationshipDefinition
	errorChan        chan error
	log              logger.Handler
}

func NewEntityRegistrationHelper(hc *models.HandlerConfig, rm *meshmodel.RegistryManager, log logger.Handler) *EntityRegistrationHelper {
	return &EntityRegistrationHelper{
		handlerConfig:    hc,
		regManager:       rm,
		componentChan:    make(chan v1alpha1.ComponentDefinition, 1),
		relationshipChan: make(chan v1alpha1.RelationshipDefinition, 1),
		errorChan:        make(chan error),
		log:              log,
	}
}

// seed the local meshmodel components
func (erh *EntityRegistrationHelper) SeedComponents() {
	// Watch channels and register components and relationships with the registry manager
	ctx, cancel := context.WithCancel(context.TODO())
	defer cancel()

	go erh.watchComponents(ctx)

	// Read component and relationship definitions from files and send them to respective channels
	erh.generateComponents("../meshmodel/components")
	erh.generateRelationships("../meshmodel/relationships")
}

// reads component definitions from files and sends them to the component channel
func (erh *EntityRegistrationHelper) generateComponents(pathToComponents string) {
	path, err := filepath.Abs(pathToComponents)
	if err != nil {
		erh.errorChan <- errors.Wrapf(err, "error while getting absolute path for generating components")
		return
	}

	err = filepath.Walk(path, func(path string, info fs.FileInfo, err error) error {
		if info == nil {
			return nil
		}

		if !info.IsDir() {
			// Read the component definition from file
			var comp v1alpha1.ComponentDefinition
			byt, err := os.ReadFile(path)
			if err != nil {
				erh.errorChan <- errors.Wrapf(err, fmt.Sprintf("unable to read file at %s", path))
				return nil
			}
			err = json.Unmarshal(byt, &comp)
			if err != nil {
				erh.errorChan <- errors.Wrapf(err, fmt.Sprintf("unmarshal json failed for %s", path))
				return nil
			}
			// Only register components that have been marked as published
			if comp.Metadata != nil && comp.Metadata["published"] == true {
				// Generate SVGs for the component and save them on the file system
				utils.WriteSVGsOnFileSystem(&comp)
				erh.componentChan <- comp
			}
		}
		return nil
	})
	if err != nil {
		erh.errorChan <- errors.Wrapf(err, "error while generating components")
	}
	return
}

// reads relationship definitions from files and sends them to the relationship channel
func (erh *EntityRegistrationHelper) generateRelationships(pathToComponents string) {
	path, err := filepath.Abs(pathToComponents)
	if err != nil {
		erh.errorChan <- errors.Wrapf(err, "error while getting absolute path for generating relationships")
		return
	}

	err = filepath.Walk(path, func(path string, info fs.FileInfo, err error) error {
		if info == nil {
			return nil
		}
		if !info.IsDir() {
			var rel v1alpha1.RelationshipDefinition
			byt, err := os.ReadFile(path)
			if err != nil {
				erh.errorChan <- errors.Wrapf(err, fmt.Sprintf("unable to read file at %s", path))
				return nil
			}
			err = json.Unmarshal(byt, &rel)
			if err != nil {
				erh.errorChan <- errors.Wrapf(err, fmt.Sprintf("unmarshal json failed for %s", path))
				return nil
			}
			erh.relationshipChan <- rel
		}
		return nil
	})
	if err != nil {
		erh.errorChan <- errors.Wrapf(err, "error while generating relationships")
	}
	return
}

// watches the component and relationship channels for incoming definitions and registers them with the registry manager
// If an error occurs, it logs the error
func (erh *EntityRegistrationHelper) watchComponents(ctx context.Context) {
	var err error
	for {
		select {
		case comp := <-erh.componentChan:
			err = erh.regManager.RegisterEntity(meshmodel.Host{
				Hostname: ArtifactHubComponentsHandler,
			}, comp)
		case rel := <-erh.relationshipChan:
			err = erh.regManager.RegisterEntity(meshmodel.Host{
				Hostname: ArtifactHubComponentsHandler,
			}, rel)

		//Watching and logging errors from error channel
		case mhErr := <-erh.errorChan:
			if err != nil {
				erh.log.Error(mhErr)
			}

		case <-ctx.Done():
			return
		}

		if err != nil {
			erh.errorChan <- err
		}
	}
}
