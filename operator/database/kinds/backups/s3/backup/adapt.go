package backup

import (
	"time"

	"github.com/zitadel/zitadel/operator/database/kinds/backups/s3/command"
	"github.com/zitadel/zitadel/pkg/databases/db"

	"github.com/zitadel/zitadel/operator"

	"github.com/caos/orbos/mntr"
	"github.com/caos/orbos/pkg/kubernetes"
	"github.com/caos/orbos/pkg/kubernetes/resources/cronjob"
	"github.com/caos/orbos/pkg/kubernetes/resources/job"
	"github.com/caos/orbos/pkg/labels"
	corev1 "k8s.io/api/core/v1"
)

const (
	defaultMode         int32 = 256
	certPath                  = "/cockroach/cockroach-certs"
	accessKeyIDPath           = "/secrets/accessaccountkey"
	secretAccessKeyPath       = "/secrets/secretaccesskey"
	sessionTokenPath          = "/secrets/sessiontoken"
	backupNameEnv             = "BACKUP_NAME"
	cronJobNamePrefix         = "backup-"
	internalSecretName        = "client-certs"
	timeout                   = 15 * time.Minute
	Normal                    = "backup"
	Instant                   = "instantbackup"
	rootSecretName            = "cockroachdb.node"
)

func AdaptFunc(
	monitor mntr.Monitor,
	backupName string,
	namespace string,
	componentLabels *labels.Component,
	checkDBReady operator.EnsureFunc,
	bucketName string,
	cron string,
	accessKeyIDName string,
	accessKeyIDKey string,
	secretAccessKeyName string,
	secretAccessKeyKey string,
	sessionTokenName string,
	sessionTokenKey string,
	region string,
	endpoint string,
	timestamp string,
	nodeselector map[string]string,
	tolerations []corev1.Toleration,
	dbConn db.Connection,
	features []string,
	image string,
) (
	queryFunc operator.QueryFunc,
	destroyFunc operator.DestroyFunc,
	err error,
) {

	backupTime := timestamp
	if timestamp == "" {
		backupTime = "$(date +%Y-%m-%dT%H:%M:%SZ)"
	}

	cmd, env := command.GetCommand(
		bucketName,
		backupName,
		backupTime,
		certPath,
		accessKeyIDPath,
		secretAccessKeyPath,
		sessionTokenPath,
		region,
		endpoint,
		dbConn,
		command.Backup,
	)

	jobSpecDef := getJobSpecDef(
		nodeselector,
		tolerations,
		accessKeyIDName,
		accessKeyIDKey,
		secretAccessKeyName,
		secretAccessKeyKey,
		sessionTokenName,
		sessionTokenKey,
		backupName,
		image,
		cmd,
		env,
	)

	destroyers := []operator.DestroyFunc{}
	queriers := []operator.QueryFunc{}

	cronJobDef := getCronJob(
		namespace,
		labels.MustForName(componentLabels, GetJobName(backupName)),
		cron,
		jobSpecDef,
	)

	destroyCJ, err := cronjob.AdaptFuncToDestroy(cronJobDef.Namespace, cronJobDef.Name)
	if err != nil {
		return nil, nil, err
	}

	queryCJ, err := cronjob.AdaptFuncToEnsure(cronJobDef)
	if err != nil {
		return nil, nil, err
	}

	jobDef := getJob(
		namespace,
		labels.MustForName(componentLabels, cronJobNamePrefix+backupName),
		jobSpecDef,
	)

	destroyJ, err := job.AdaptFuncToDestroy(jobDef.Namespace, jobDef.Name)
	if err != nil {
		return nil, nil, err
	}

	queryJ, err := job.AdaptFuncToEnsure(jobDef)
	if err != nil {
		return nil, nil, err
	}

	for _, feature := range features {
		switch feature {
		case Normal:
			destroyers = append(destroyers,
				operator.ResourceDestroyToZitadelDestroy(destroyCJ),
			)
			queriers = append(queriers,
				operator.EnsureFuncToQueryFunc(checkDBReady),
				operator.ResourceQueryToZitadelQuery(queryCJ),
			)
		case Instant:
			destroyers = append(destroyers,
				operator.ResourceDestroyToZitadelDestroy(destroyJ),
			)
			queriers = append(queriers,
				operator.EnsureFuncToQueryFunc(checkDBReady),
				operator.ResourceQueryToZitadelQuery(queryJ),
			)
		}
	}

	return func(k8sClient kubernetes.ClientInt, queried map[string]interface{}) (operator.EnsureFunc, error) {
			return operator.QueriersToEnsureFunc(monitor, false, queriers, k8sClient, queried)
		},
		operator.DestroyersToDestroyFunc(monitor, destroyers),
		nil
}

func GetJobName(backupName string) string {
	return cronJobNamePrefix + backupName
}