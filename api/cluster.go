package api

import (
	"fmt"
	"github.com/banzaicloud/banzai-types/components"
	"github.com/banzaicloud/banzai-types/constants"
	"github.com/banzaicloud/pipeline/cluster"
	"github.com/banzaicloud/pipeline/config"
	"github.com/banzaicloud/pipeline/helm"
	"github.com/banzaicloud/pipeline/model"
	"github.com/banzaicloud/pipeline/pods"
	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"net/http"
)

// TODO se who will win
var logger *logrus.Logger
var log *logrus.Entry

// Simple init for logging
func init() {
	logger = config.Logger()
	log = logger.WithFields(logrus.Fields{"action": "Cluster"})
}

//This is to restrict other query TODO investigate to just pass the hasmap
func ParseField(c *gin.Context) map[string]interface{} {
	value := c.Param("id")
	field := c.DefaultQuery("field", "id")
	filter := make(map[string]interface{})
	filter[field] = value
	return filter
}

// Simple getter to build commonCluster object this handles error messages directly
func GetCommonClusterFromRequest(c *gin.Context) (cluster.CommonCluster, bool) {
	filter := ParseField(c)

	//TODO check if error handling is enough
	modelCluster, err := model.QueryCluster(filter)
	if err != nil {
		log.Error(err)
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Code:    http.StatusBadRequest,
			Message: "Error parsing request",
			Error:   err.Error(),
		})
		return nil, false
	}
	commonCLuster, err := cluster.GetCommonClusterFromModel(modelCluster)
	if err != nil {
		log.Error(err)
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Code:    http.StatusBadRequest,
			Message: "Error parsing request",
			Error:   err.Error(),
		})
		return nil, false
	}
	return commonCLuster, true
}

// CreateCluster creates a K8S cluster in the cloud
func CreateCluster(c *gin.Context) {
	log := logger.WithFields(logrus.Fields{"action": constants.TagCreateCluster})
	//TODO refactor logging here

	log.Info("Cluster creation stared")

	log.Debug("Bind json into CreateClusterRequest struct")
	// bind request body to struct
	var createClusterRequest components.CreateClusterRequest
	if err := c.BindJSON(&createClusterRequest); err != nil {
		log.Error(errors.Wrap(err, "Error parsing request"))
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Code:    http.StatusBadRequest,
			Message: "Error parsing request",
			Error:   err.Error(),
		})
		return
	}
	log.Debug("Parsing request succeeded")

	log.Info("Searching entry with name: ", createClusterRequest.Name)

	// check exists cluster name
	var existingCluster model.ClusterModel
	database := model.GetDB()
	database.Raw("SELECT * FROM "+model.ClusterModel.TableName(existingCluster)+" WHERE name = ?;",
		createClusterRequest.Name).Scan(&existingCluster)

	////TODO check if error handling is enough
	//existingCluster, err := model.QueryCluster(filter)
	//if err != nil {
	//	log.Error(err)
	//	c.JSON(http.StatusBadRequest, ErrorResponse{
	//		Code:    http.StatusBadRequest,
	//		Message: "Error parsing request",
	//		Error:   err.Error(),
	//	})
	//	return
	//}

	if existingCluster.ID != 0 {
		// duplicated entry
		err := fmt.Errorf("duplicate entry: %s", existingCluster.Name)
		log.Error(err)
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Code:    http.StatusBadRequest,
			Message: err.Error(),
			Error:   err.Error(),
		})
		return
	}

	log.Info("Creating new entry with cloud type: ", createClusterRequest.Cloud)

	var commonCLuster cluster.CommonCluster

	// TODO check validation
	commonCLuster, err := cluster.CreateCommonClusterFromRequest(&createClusterRequest)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Code:    http.StatusBadRequest,
			Message: err.Error(),
			Error:   err.Error(),
		})
		return
	}
	// This is the common part of cluster flow

	// todo if the cluster save into db before validate, the cluster cloud not delete from database
	// Persist the cluster in Database
	err = commonCLuster.Persist()
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Code:    http.StatusBadRequest,
			Message: err.Error(),
			Error:   err.Error(),
		})
		return
	}

	// Create cluster
	err = commonCLuster.CreateCluster()
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Code:    http.StatusBadRequest,
			Message: err.Error(),
			Error:   err.Error(),
		})
		return
	}

	// Apply PostHooks
	// These are hardcoded poshooks maybe we will want a bit more dynamic
	postHookFunctions := []func(commonCluster cluster.CommonCluster){
		cluster.GetConfigPostHook,
		cluster.UpdatePrometheusPostHook,
		cluster.InstallHelmPostHook,
		cluster.InstallIngressControllerPostHook,
	}
	go cluster.RunPostHooks(postHookFunctions, commonCLuster)

	return
}

// GetClusterStatus retrieves the cluster status
func GetClusterStatus(c *gin.Context) {
	log := logger.WithFields(logrus.Fields{"tag": constants.TagGetClusterStatus})

	commonCluster, ok := GetCommonClusterFromRequest(c)
	if ok != true {
		return
	}

	response, err := commonCluster.GetStatus()
	if err != nil {
		log.Error(err)
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Code:    http.StatusBadRequest,
			Message: "Error parsing request",
			Error:   err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, response)
	return
}

// FetchClusterConfig fetches a cluster config
func GetClusterConfig(c *gin.Context) {
	log := logger.WithFields(logrus.Fields{"tag": "GetClusterConfig"})
	commonCluster, ok := GetCommonClusterFromRequest(c)
	if ok != true {
		return
	}
	config, err := commonCluster.GetK8sConfig()
	if err != nil {
		log.Error(err)
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Code:    http.StatusBadRequest,
			Message: "Error parsing request",
			Error:   err.Error(),
		})
		return
	}
	contentType := c.NegotiateFormat(gin.MIMEPlain, gin.MIMEJSON)
	log.Debug("Content-Type: ", contentType)
	switch contentType {
	case gin.MIMEJSON:
		c.JSON(http.StatusOK, components.GetClusterConfigResponse{
			Status: http.StatusOK,
			Data:   string(*config),
		})
	default:
		c.String(http.StatusOK, string(*config))
	}
	return
}

// UpdateCluster updates a K8S cluster in the cloud (e.g. autoscale)
func UpdateCluster(c *gin.Context) {
	log := logger.WithFields(logrus.Fields{"tag": constants.TagGetClusterInfo})

	// bind request body to UpdateClusterRequest struct
	var updateRequest *components.UpdateClusterRequest
	if err := c.BindJSON(&updateRequest); err != nil {
		log.Error(err)
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Code:    http.StatusBadRequest,
			Message: "Error parsing request",
			Error:   err.Error(),
		})
		return
	}
	commonCluster, ok := GetCommonClusterFromRequest(c)
	if ok != true {
		return
	}

	log.Info("Add default values to request if necessarily")

	// set default
	commonCluster.AddDefaultsToUpdate(updateRequest)

	log.Info("Check equality")
	if err := commonCluster.CheckEqualityToUpdate(updateRequest); err != nil {
		log.Errorf("Check changes failed: %s", err.Error())
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Code:    http.StatusBadRequest,
			Message: err.Error(),
			Error:   err.Error(),
		})
		return
	}

	if err := updateRequest.Validate(); err != nil {
		log.Errorf("Validation failed: %s", err.Error())
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Code:    http.StatusBadRequest,
			Message: err.Error(),
			Error:   err.Error(),
		})
		return
	}

	// TODO check if validation can be applied sooner
	err := commonCluster.UpdateCluster(updateRequest)
	if err != nil {
		// validation failed
		log.Errorf("Update failed: %s", err.Error())
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Code:    http.StatusBadRequest,
			Message: err.Error(),
			Error:   err.Error(),
		})
		return
	}

	c.JSON(http.StatusAccepted, components.UpdateClusterResponse{
		Status: http.StatusAccepted,
	})
}

// DeleteCluster deletes a K8S cluster from the cloud
func DeleteCluster(c *gin.Context) {
	log := logger.WithFields(logrus.Fields{"tag": constants.TagDeleteCluster})
	commonCluster, ok := GetCommonClusterFromRequest(c)
	if ok != true {
		return
	}
	log.Info("Delete cluster start")

	config, err := commonCluster.GetK8sConfig()
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Code:    http.StatusBadRequest,
			Message: err.Error(),
			Error:   err.Error(),
		})
		return
	}

	err = helm.DeleteAllDeployment(config)
	if err != nil {
		log.Errorf("Problem deleting deployment: %s", err)
	}

	err = commonCluster.DeleteCluster()

	// todo error handling

	// Asyncron update prometheus
	go cluster.UpdatePrometheus()

	c.JSON(http.StatusAccepted, components.DeleteClusterResponse{
		Status:     http.StatusAccepted,
		Name:       commonCluster.GetName(),
		Message:    "Cluster deleted succesfully",
		ResourceID: commonCluster.GetID(),
	})
	return
}

// FetchClusters fetches all the K8S clusters from the cloud
func FetchClusters(c *gin.Context) {
	log := logger.WithFields(logrus.Fields{"tag": constants.TagDeleteCluster})
	log.Info("Fetching clusters")

	var clusters []cluster.CommonCluster //TODO change this to CommonClusterStatus
	db := model.GetDB()
	db.Find(&clusters)

	if len(clusters) < 1 {
		message := "No clusters found"
		log.Info(message)
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Code:    http.StatusBadRequest,
			Message: message,
			Error:   message,
		})
		return
	}
	var response []*components.GetClusterStatusResponse
	for _, commonCluster := range clusters {
		status, err := commonCluster.GetStatus()
		if err != nil {
			//TODO we want skip or return error?
			log.Errorf("get status failed for %s", commonCluster.GetName())
		}
		log.Debugf("Append cluster to list: %s", commonCluster.GetName())
		response = append(response, status)
	}
	c.JSON(http.StatusOK, response)
}

// FetchCluster fetch a K8S cluster in the cloud
func FetchCluster(c *gin.Context) {
	log := logger.WithFields(logrus.Fields{"tag": constants.TagGetClusterStatus})
	commonCluster, ok := GetCommonClusterFromRequest(c)
	if ok != true {
		return
	}
	log.Info("getting cluster info")
	status, err := commonCluster.GetStatus()
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Code:    http.StatusBadRequest,
			Message: err.Error(),
			Error:   err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, status)
}

//Status
func Status(c *gin.Context) {
	var clusters []cluster.CommonCluster
	log := logger.WithFields(logrus.Fields{"tag": constants.TagStatus})
	db := model.GetDB()
	db.Find(&clusters)

	if len(clusters) == 0 {
		c.JSON(http.StatusOK, gin.H{"No running clusters found.": http.StatusOK})
	} else {
		var clusterStatuses []pods.ClusterStatusResponse
		for _, cl := range clusters {
			log.Info("Start listing pods / cluster")
			var clusterStatusResponse pods.ClusterStatusResponse
			clusterStatusResponse, err := pods.ListPodsForCluster(&cl)
			if err == nil {
				clusterStatuses = append(clusterStatuses, clusterStatusResponse)
			} else {
				banzaiUtils.LogError(utils.TagStatus, err)
			}

		}
		c.JSON(http.StatusOK, gin.H{"clusterStatuses": clusterStatuses})
	}

}
