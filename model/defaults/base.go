package defaults

import (
	"github.com/banzaicloud/banzai-types/constants"
	"time"
	"github.com/sirupsen/logrus"
	"github.com/banzaicloud/pipeline/config"
	"github.com/banzaicloud/pipeline/model"
	"github.com/banzaicloud/banzai-types/components"
)

// TODO se who will win
var logger *logrus.Logger
var log *logrus.Entry

const (
	defaultAmazonProfileTablaName = "amazon_default_profile"
	defaultAzureProfileTablaName  = "azure_default_profile"
	defaultGoogleProfileTablaName = "google_default_profile"
)

// Simple init for logging
func init() {
	logger = config.Logger()
	log = logger.WithFields(logrus.Fields{"action": constants.TagGetDefaults})
}

func SetDefaultValues() {

	defaults := GetDefaultProfiles()
	for _, d := range defaults {
		if !d.IsDefinedBefore() {
			log.Infof("%s default table NOT contains the default values. Fill it...", d.GetType())
			if err := d.SaveInstance(); err != nil {
				log.Errorf("Could not save default values[%s]: %s", d.GetType(), err.Error())
			}
		} else {
			log.Infof("%s default table already contains the default values", d.GetType())
		}
	}
}

type ClusterProfile interface {
	IsDefinedBefore() bool
	SaveInstance() error
	GetType() string
	GetProfile() *components.ClusterProfileRespone
}

type DefaultModel struct {
	Name      string `gorm:"primary_key"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

func save(i interface{}) error {
	database := model.GetDB()
	if err := database.Save(i).Error; err != nil {
		return err
	}
	return nil
}

func loadFirst(output interface{}) {
	model.GetDB().First(output)
}

func GetDefaultProfiles() []ClusterProfile {
	var defaults []ClusterProfile
	defaults = append(defaults,
		&AWSProfile{DefaultModel: DefaultModel{Name: "default"},},
		&AKSProfile{DefaultModel: DefaultModel{Name: "default"},},
		&GKEProfile{DefaultModel: DefaultModel{Name: "default"},})
	return defaults
}

func GetAllProfiles(cloudType string) ([]ClusterProfile, error) {

	var defaults []ClusterProfile
	db := model.GetDB()

	switch cloudType {

	case constants.Amazon:
		var awsProfiles []AWSProfile
		db.Find(&awsProfiles)
		for i := range awsProfiles {
			defaults = append(defaults, &awsProfiles[i])
		}

	case constants.Azure:
		var aksProfiles []AKSProfile
		db.Find(&aksProfiles)
		for i := range aksProfiles {
			defaults = append(defaults, &aksProfiles[i])
		}

	case constants.Google:
		var gkeProfiles []GKEProfile
		db.Find(&gkeProfiles)
		for i := range gkeProfiles {
			defaults = append(defaults, &gkeProfiles[i])
		}

	default:
		return nil, constants.NotSupportedCloudType
	}

	return defaults, nil

}
