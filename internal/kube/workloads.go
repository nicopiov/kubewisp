package kube

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	restartedAtAnnotation     = "kubectl.kubernetes.io/restartedAt"
	recentCronJobHistoryLimit = 20
)

type WorkloadService interface {
	List(ctx context.Context, namespace string) ([]WorkloadSummary, error)
	RolloutRestart(ctx context.Context, namespace, kind, name string) error
	Describe(ctx context.Context, namespace, kind, name string) (WorkloadDetails, error)
	Pods(ctx context.Context, namespace, kind, name string) ([]PodSummary, error)
	DescribeCronJob(ctx context.Context, namespace, name string) (CronJobDetails, error)
	SetCronJobSuspended(ctx context.Context, namespace, name string, suspended bool) error
}

type WorkloadSummary struct {
	Kind               string
	Name               string
	Ready              int32
	Desired            int32
	Updated            int32
	Available          int32
	Schedule           string
	Suspended          bool
	Active             int32
	LastScheduleTime   time.Time
	LastSuccessfulTime time.Time
	CreatedAt          time.Time
}

type CronJobDetails struct {
	WorkloadSummary
	ConcurrencyPolicy      string
	SuccessfulHistoryLimit *int32
	FailedHistoryLimit     *int32
	StartingDeadline       *int64
	Jobs                   []JobSummary
}

type WorkloadDetails struct {
	WorkloadSummary
	Strategy       string
	Selector       string
	ServiceAccount string
	Containers     []string
	Conditions     []WorkloadCondition
}

type WorkloadCondition struct {
	Type    string
	Status  string
	Reason  string
	Message string
}

type JobSummary struct {
	Name           string
	Status         string
	Active         int32
	Succeeded      int32
	Failed         int32
	CreatedAt      time.Time
	StartTime      time.Time
	CompletionTime time.Time
}

type Workloads struct {
	factory ClientFactory
	now     func() time.Time
}

func NewWorkloadService() *Workloads {
	return &Workloads{factory: NewKubeconfigClientFactory(), now: time.Now}
}

func NewWorkloadServiceWithFactory(factory ClientFactory) *Workloads {
	return &Workloads{factory: factory, now: time.Now}
}

func (s *Workloads) List(ctx context.Context, namespace string) ([]WorkloadSummary, error) {
	client, err := s.client()
	if err != nil {
		return nil, err
	}
	var deployments *appsv1.DeploymentList
	var statefulSets *appsv1.StatefulSetList
	var daemonSets *appsv1.DaemonSetList
	var cronJobs *batchv1.CronJobList
	var deploymentErr, statefulSetErr, daemonSetErr, cronJobErr error
	var wait sync.WaitGroup
	wait.Add(4)
	go func() {
		defer wait.Done()
		deployments, deploymentErr = client.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
	}()
	go func() {
		defer wait.Done()
		statefulSets, statefulSetErr = client.AppsV1().StatefulSets(namespace).List(ctx, metav1.ListOptions{})
	}()
	go func() {
		defer wait.Done()
		daemonSets, daemonSetErr = client.AppsV1().DaemonSets(namespace).List(ctx, metav1.ListOptions{})
	}()
	go func() {
		defer wait.Done()
		cronJobs, cronJobErr = client.BatchV1().CronJobs(namespace).List(ctx, metav1.ListOptions{})
	}()
	wait.Wait()
	if deploymentErr != nil {
		return nil, diagnose(fmt.Sprintf("list deployments in namespace %q", namespace), deploymentErr)
	}
	if statefulSetErr != nil {
		return nil, diagnose(fmt.Sprintf("list statefulsets in namespace %q", namespace), statefulSetErr)
	}
	if daemonSetErr != nil {
		return nil, diagnose(fmt.Sprintf("list daemonsets in namespace %q", namespace), daemonSetErr)
	}
	if cronJobErr != nil {
		return nil, diagnose(fmt.Sprintf("list cronjobs in namespace %q", namespace), cronJobErr)
	}

	var workloads []WorkloadSummary
	for _, item := range deployments.Items {
		workloads = append(workloads, WorkloadSummary{
			Kind:      "Deployment",
			Name:      item.Name,
			Ready:     item.Status.ReadyReplicas,
			Desired:   desiredReplicas(item.Spec.Replicas),
			Updated:   item.Status.UpdatedReplicas,
			Available: item.Status.AvailableReplicas,
			CreatedAt: item.CreationTimestamp.Time,
		})
	}
	for _, item := range statefulSets.Items {
		workloads = append(workloads, WorkloadSummary{
			Kind:      "StatefulSet",
			Name:      item.Name,
			Ready:     item.Status.ReadyReplicas,
			Desired:   desiredReplicas(item.Spec.Replicas),
			Updated:   item.Status.UpdatedReplicas,
			Available: item.Status.AvailableReplicas,
			CreatedAt: item.CreationTimestamp.Time,
		})
	}
	for _, item := range daemonSets.Items {
		workloads = append(workloads, WorkloadSummary{
			Kind:      "DaemonSet",
			Name:      item.Name,
			Ready:     item.Status.NumberReady,
			Desired:   item.Status.DesiredNumberScheduled,
			Updated:   item.Status.UpdatedNumberScheduled,
			Available: item.Status.NumberAvailable,
			CreatedAt: item.CreationTimestamp.Time,
		})
	}
	for index := range cronJobs.Items {
		workloads = append(workloads, summarizeCronJob(&cronJobs.Items[index]))
	}
	sort.Slice(workloads, func(i, j int) bool {
		if workloads[i].Kind == workloads[j].Kind {
			return workloads[i].Name < workloads[j].Name
		}
		return workloads[i].Kind < workloads[j].Kind
	})
	return workloads, nil
}

func SupportsRolloutRestart(kind string) bool {
	switch strings.ToLower(kind) {
	case "deployment", "deployments", "statefulset", "statefulsets", "daemonset", "daemonsets":
		return true
	default:
		return false
	}
}

func (s *Workloads) Describe(ctx context.Context, namespace, kind, name string) (WorkloadDetails, error) {
	client, err := s.client()
	if err != nil {
		return WorkloadDetails{}, err
	}
	switch strings.ToLower(kind) {
	case "deployment", "deployments":
		item, err := client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return WorkloadDetails{}, diagnose(fmt.Sprintf("get deployment %q in namespace %q", name, namespace), err)
		}
		details := WorkloadDetails{
			WorkloadSummary: WorkloadSummary{
				Kind: "Deployment", Name: item.Name, Ready: item.Status.ReadyReplicas,
				Desired: desiredReplicas(item.Spec.Replicas), Updated: item.Status.UpdatedReplicas,
				Available: item.Status.AvailableReplicas, CreatedAt: item.CreationTimestamp.Time,
			},
			Strategy:       string(item.Spec.Strategy.Type),
			Selector:       metav1.FormatLabelSelector(item.Spec.Selector),
			ServiceAccount: item.Spec.Template.Spec.ServiceAccountName,
			Containers:     templateContainers(item.Spec.Template.Spec.Containers),
		}
		for _, condition := range item.Status.Conditions {
			details.Conditions = append(details.Conditions, WorkloadCondition{
				Type: string(condition.Type), Status: string(condition.Status),
				Reason: condition.Reason, Message: condition.Message,
			})
		}
		return details, nil
	case "statefulset", "statefulsets":
		item, err := client.AppsV1().StatefulSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return WorkloadDetails{}, diagnose(fmt.Sprintf("get statefulset %q in namespace %q", name, namespace), err)
		}
		details := WorkloadDetails{
			WorkloadSummary: WorkloadSummary{
				Kind: "StatefulSet", Name: item.Name, Ready: item.Status.ReadyReplicas,
				Desired: desiredReplicas(item.Spec.Replicas), Updated: item.Status.UpdatedReplicas,
				Available: item.Status.AvailableReplicas, CreatedAt: item.CreationTimestamp.Time,
			},
			Strategy:       string(item.Spec.UpdateStrategy.Type),
			Selector:       metav1.FormatLabelSelector(item.Spec.Selector),
			ServiceAccount: item.Spec.Template.Spec.ServiceAccountName,
			Containers:     templateContainers(item.Spec.Template.Spec.Containers),
		}
		for _, condition := range item.Status.Conditions {
			details.Conditions = append(details.Conditions, WorkloadCondition{
				Type: string(condition.Type), Status: string(condition.Status),
				Reason: condition.Reason, Message: condition.Message,
			})
		}
		return details, nil
	case "daemonset", "daemonsets":
		item, err := client.AppsV1().DaemonSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return WorkloadDetails{}, diagnose(fmt.Sprintf("get daemonset %q in namespace %q", name, namespace), err)
		}
		details := WorkloadDetails{
			WorkloadSummary: WorkloadSummary{
				Kind: "DaemonSet", Name: item.Name, Ready: item.Status.NumberReady,
				Desired: item.Status.DesiredNumberScheduled, Updated: item.Status.UpdatedNumberScheduled,
				Available: item.Status.NumberAvailable, CreatedAt: item.CreationTimestamp.Time,
			},
			Strategy:       string(item.Spec.UpdateStrategy.Type),
			Selector:       metav1.FormatLabelSelector(item.Spec.Selector),
			ServiceAccount: item.Spec.Template.Spec.ServiceAccountName,
			Containers:     templateContainers(item.Spec.Template.Spec.Containers),
		}
		for _, condition := range item.Status.Conditions {
			details.Conditions = append(details.Conditions, WorkloadCondition{
				Type: string(condition.Type), Status: string(condition.Status),
				Reason: condition.Reason, Message: condition.Message,
			})
		}
		return details, nil
	default:
		return WorkloadDetails{}, fmt.Errorf("workload kind %q does not have replica workload details", kind)
	}
}

func (s *Workloads) Pods(ctx context.Context, namespace, kind, name string) ([]PodSummary, error) {
	client, err := s.client()
	if err != nil {
		return nil, err
	}
	var selector *metav1.LabelSelector
	switch strings.ToLower(kind) {
	case "deployment", "deployments":
		item, err := client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, diagnose(fmt.Sprintf("get deployment %q in namespace %q", name, namespace), err)
		}
		selector = item.Spec.Selector
	case "statefulset", "statefulsets":
		item, err := client.AppsV1().StatefulSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, diagnose(fmt.Sprintf("get statefulset %q in namespace %q", name, namespace), err)
		}
		selector = item.Spec.Selector
	case "daemonset", "daemonsets":
		item, err := client.AppsV1().DaemonSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, diagnose(fmt.Sprintf("get daemonset %q in namespace %q", name, namespace), err)
		}
		selector = item.Spec.Selector
	default:
		return nil, fmt.Errorf("workload kind %q does not manage a pod set", kind)
	}
	if selector == nil {
		return nil, fmt.Errorf("%s/%s does not define a pod selector", kind, name)
	}
	labelSelector, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		return nil, fmt.Errorf("build pod selector for %s/%s: %w", kind, name, err)
	}
	list, err := client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: labelSelector.String()})
	if err != nil {
		return nil, diagnose(fmt.Sprintf("list pods for %s/%s in namespace %q", kind, name, namespace), err)
	}
	pods := make([]PodSummary, 0, len(list.Items))
	for index := range list.Items {
		pods = append(pods, summarizePod(&list.Items[index]))
	}
	sort.Slice(pods, func(i, j int) bool {
		return pods[i].Name < pods[j].Name
	})
	return pods, nil
}

func (s *Workloads) DescribeCronJob(ctx context.Context, namespace, name string) (CronJobDetails, error) {
	client, err := s.client()
	if err != nil {
		return CronJobDetails{}, err
	}
	cronJob, err := client.BatchV1().CronJobs(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return CronJobDetails{}, diagnose(fmt.Sprintf("get cronjob %q in namespace %q", name, namespace), err)
	}
	jobs, err := client.BatchV1().Jobs(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return CronJobDetails{}, diagnose(fmt.Sprintf("list jobs for cronjob %q in namespace %q", name, namespace), err)
	}
	details := CronJobDetails{
		WorkloadSummary:        summarizeCronJob(cronJob),
		ConcurrencyPolicy:      string(cronJob.Spec.ConcurrencyPolicy),
		SuccessfulHistoryLimit: cronJob.Spec.SuccessfulJobsHistoryLimit,
		FailedHistoryLimit:     cronJob.Spec.FailedJobsHistoryLimit,
		StartingDeadline:       cronJob.Spec.StartingDeadlineSeconds,
	}
	for index := range jobs.Items {
		job := &jobs.Items[index]
		if ownedBy(job.OwnerReferences, "CronJob", name) {
			details.Jobs = append(details.Jobs, summarizeJob(job))
		}
	}
	sort.Slice(details.Jobs, func(i, j int) bool {
		return jobLatestTime(details.Jobs[i]).After(jobLatestTime(details.Jobs[j]))
	})
	if len(details.Jobs) > recentCronJobHistoryLimit {
		details.Jobs = details.Jobs[:recentCronJobHistoryLimit]
	}
	return details, nil
}

func (s *Workloads) SetCronJobSuspended(ctx context.Context, namespace, name string, suspended bool) error {
	client, err := s.client()
	if err != nil {
		return err
	}
	cronJob, err := client.BatchV1().CronJobs(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return diagnose(fmt.Sprintf("get cronjob %q in namespace %q", name, namespace), err)
	}
	cronJob.Spec.Suspend = &suspended
	if _, err := client.BatchV1().CronJobs(namespace).Update(ctx, cronJob, metav1.UpdateOptions{}); err != nil {
		action := "resume"
		if suspended {
			action = "suspend"
		}
		return diagnose(fmt.Sprintf("%s cronjob %q in namespace %q", action, name, namespace), err)
	}
	return nil
}

func (s *Workloads) RolloutRestart(ctx context.Context, namespace, kind, name string) error {
	client, err := s.client()
	if err != nil {
		return err
	}
	restartedAt := s.now().UTC().Format(time.RFC3339)
	switch strings.ToLower(kind) {
	case "deployment", "deployments":
		workload, err := client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return diagnose(fmt.Sprintf("get deployment %q in namespace %q", name, namespace), err)
		}
		setRestartedAt(&workload.Spec.Template.ObjectMeta, restartedAt)
		if _, err := client.AppsV1().Deployments(namespace).Update(ctx, workload, metav1.UpdateOptions{}); err != nil {
			return diagnose(fmt.Sprintf("restart deployment %q in namespace %q", name, namespace), err)
		}
	case "statefulset", "statefulsets":
		workload, err := client.AppsV1().StatefulSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return diagnose(fmt.Sprintf("get statefulset %q in namespace %q", name, namespace), err)
		}
		setRestartedAt(&workload.Spec.Template.ObjectMeta, restartedAt)
		if _, err := client.AppsV1().StatefulSets(namespace).Update(ctx, workload, metav1.UpdateOptions{}); err != nil {
			return diagnose(fmt.Sprintf("restart statefulset %q in namespace %q", name, namespace), err)
		}
	case "daemonset", "daemonsets":
		workload, err := client.AppsV1().DaemonSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return diagnose(fmt.Sprintf("get daemonset %q in namespace %q", name, namespace), err)
		}
		setRestartedAt(&workload.Spec.Template.ObjectMeta, restartedAt)
		if _, err := client.AppsV1().DaemonSets(namespace).Update(ctx, workload, metav1.UpdateOptions{}); err != nil {
			return diagnose(fmt.Sprintf("restart daemonset %q in namespace %q", name, namespace), err)
		}
	default:
		return fmt.Errorf("workload kind %q is not supported", kind)
	}
	return nil
}

func summarizeCronJob(item *batchv1.CronJob) WorkloadSummary {
	return WorkloadSummary{
		Kind:               "CronJob",
		Name:               item.Name,
		Schedule:           item.Spec.Schedule,
		Suspended:          item.Spec.Suspend != nil && *item.Spec.Suspend,
		Active:             int32(len(item.Status.Active)),
		LastScheduleTime:   timeValue(item.Status.LastScheduleTime),
		LastSuccessfulTime: timeValue(item.Status.LastSuccessfulTime),
		CreatedAt:          item.CreationTimestamp.Time,
	}
}

func summarizeJob(item *batchv1.Job) JobSummary {
	return JobSummary{
		Name:           item.Name,
		Status:         jobStatus(item),
		Active:         item.Status.Active,
		Succeeded:      item.Status.Succeeded,
		Failed:         item.Status.Failed,
		CreatedAt:      item.CreationTimestamp.Time,
		StartTime:      timeValue(item.Status.StartTime),
		CompletionTime: timeValue(item.Status.CompletionTime),
	}
}

func jobStatus(item *batchv1.Job) string {
	for _, condition := range item.Status.Conditions {
		if condition.Status != "True" {
			continue
		}
		switch condition.Type {
		case batchv1.JobComplete:
			return "Completed"
		case batchv1.JobFailed:
			return "Failed"
		}
	}
	if item.Status.Active > 0 {
		return "Running"
	}
	if item.Status.Failed > 0 {
		return "Failed"
	}
	if item.Status.Succeeded > 0 {
		return "Completed"
	}
	return "Pending"
}

func jobLatestTime(job JobSummary) time.Time {
	if !job.CompletionTime.IsZero() {
		return job.CompletionTime
	}
	if !job.StartTime.IsZero() {
		return job.StartTime
	}
	return job.CreatedAt
}

func ownedBy(references []metav1.OwnerReference, kind, name string) bool {
	for _, reference := range references {
		if reference.Kind == kind && reference.Name == name {
			return true
		}
	}
	return false
}

func (s *Workloads) client() (kubernetes.Interface, error) {
	client, err := s.factory.Client()
	if err != nil {
		return nil, err
	}
	return client, nil
}

func desiredReplicas(replicas *int32) int32 {
	if replicas == nil {
		return 1
	}
	return *replicas
}

func templateContainers(containers []corev1.Container) []string {
	values := make([]string, 0, len(containers))
	for _, container := range containers {
		values = append(values, container.Name+" | image="+container.Image)
	}
	return values
}

func timeValue(value *metav1.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return value.Time
}

func setRestartedAt(meta *metav1.ObjectMeta, value string) {
	if meta.Annotations == nil {
		meta.Annotations = make(map[string]string)
	}
	meta.Annotations[restartedAtAnnotation] = value
}
