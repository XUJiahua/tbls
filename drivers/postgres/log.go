package postgres

import (
	"os"

	"github.com/sirupsen/logrus"
)

var log = logrus.New()

func init() {
	log.SetOutput(os.Stderr)
	log.SetFormatter(&logrus.TextFormatter{
		DisableTimestamp: true,
	})

	if os.Getenv("DEBUG") == "1" || os.Getenv("TBLS_DEBUG") == "1" {
		log.SetLevel(logrus.DebugLevel)
	} else {
		log.SetLevel(logrus.WarnLevel)
	}
}
