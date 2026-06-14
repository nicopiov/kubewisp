package kube

import (
	"context"
	"reflect"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestWorkloadsListSummarizesKinds(t *testing.T) {
	t.Parallel()

	replicas := int32(3)
	suspended := true
	lastSchedule := metav1.NewTime(time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC))
	lastSuccess := metav1.NewTime(time.Date(2026, 6, 12, 9, 0, 0, 0, time.UTC))
	client := fake.NewSimpleClientset(
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "apps"},
			Spec:       appsv1.DeploymentSpec{Replicas: &replicas},
			Status:     appsv1.DeploymentStatus{ReadyReplicas: 2, UpdatedReplicas: 3, AvailableReplicas: 2},
		},
		&appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "apps"},
			Spec:       appsv1.StatefulSetSpec{Replicas: &replicas, ServiceName: "db", Selector: &metav1.LabelSelector{}},
			Status:     appsv1.StatefulSetStatus{ReadyReplicas: 3, UpdatedReplicas: 3, AvailableReplicas: 3},
		},
		&appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{Name: "agent", Namespace: "apps"},
			Spec:       appsv1.DaemonSetSpec{Selector: &metav1.LabelSelector{}, Template: corev1.PodTemplateSpec{}},
			Status: appsv1.DaemonSetStatus{
				DesiredNumberScheduled: 4,
				NumberReady:            4,
				UpdatedNumberScheduled: 4,
				NumberAvailable:        4,
			},
		},
		&batchv1.CronJob{
			ObjectMeta: metav1.ObjectMeta{Name: "cleanup", Namespace: "apps"},
			Spec: batchv1.CronJobSpec{
				Schedule: "0 * * * *",
				Suspend:  &suspended,
			},
			Status: batchv1.CronJobStatus{
				Active:             []corev1.ObjectReference{{Name: "cleanup-123"}},
				LastScheduleTime:   &lastSchedule,
				LastSuccessfulTime: &lastSuccess,
			},
		},
		&corev1.Event{
			ObjectMeta:     metav1.ObjectMeta{Name: "api-failed", Namespace: "apps"},
			InvolvedObject: corev1.ObjectReference{Kind: "Deployment", Name: "api"},
			Type:           corev1.EventTypeWarning,
			Count:          2,
		},
	)

	workloads, err := NewWorkloadServiceWithFactory(fakeFactory{client: client}).List(context.Background(), "apps")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	got := []string{
		workloads[0].Kind + "/" + workloads[0].Name,
		workloads[1].Kind + "/" + workloads[1].Name,
		workloads[2].Kind + "/" + workloads[2].Name,
		workloads[3].Kind + "/" + workloads[3].Name,
	}
	want := []string{"CronJob/cleanup", "DaemonSet/agent", "Deployment/api", "StatefulSet/db"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("workloads = %#v, want %#v", got, want)
	}
	if workloads[2].Ready != 2 || workloads[2].Desired != 3 || workloads[2].WarningCount != 2 {
		t.Fatalf("deployment summary = %#v", workloads[2])
	}
	if workloads[0].Schedule != "0 * * * *" || workloads[0].Active != 1 || !workloads[0].Suspended ||
		!workloads[0].LastScheduleTime.Equal(lastSchedule.Time) || !workloads[0].LastSuccessfulTime.Equal(lastSuccess.Time) {
		t.Fatalf("cronjob summary = %#v", workloads[0])
	}
}

func TestSupportsRolloutRestart(t *testing.T) {
	t.Parallel()

	if !SupportsRolloutRestart("Deployment") {
		t.Fatal("Deployment should support rollout restart")
	}
	if SupportsRolloutRestart("CronJob") {
		t.Fatal("CronJob should not support rollout restart")
	}
}

func TestWorkloadsDescribeReplicaController(t *testing.T) {
	t.Parallel()

	replicas := int32(3)
	client := fake.NewSimpleClientset(&appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "apps"},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "api"}},
			Strategy: appsv1.DeploymentStrategy{Type: appsv1.RollingUpdateDeploymentStrategyType},
			Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
				ServiceAccountName: "api",
				Containers:         []corev1.Container{{Name: "app", Image: "example/api:v1"}},
			}},
		},
		Status: appsv1.DeploymentStatus{
			ReadyReplicas: 2, UpdatedReplicas: 3, AvailableReplicas: 2,
			Conditions: []appsv1.DeploymentCondition{{
				Type: appsv1.DeploymentAvailable, Status: corev1.ConditionTrue,
				Reason: "MinimumReplicasAvailable",
			}},
		},
	})

	details, err := NewWorkloadServiceWithFactory(fakeFactory{client: client}).Describe(
		context.Background(),
		"apps",
		"Deployment",
		"api",
	)
	if err != nil {
		t.Fatalf("Describe() error = %v", err)
	}
	if details.Ready != 2 || details.Desired != 3 || details.Strategy != "RollingUpdate" ||
		details.Selector != "app=api" || details.ServiceAccount != "api" {
		t.Fatalf("details = %#v", details)
	}
	if !reflect.DeepEqual(details.Containers, []string{"app | image=example/api:v1"}) ||
		len(details.Conditions) != 1 || details.Conditions[0].Reason != "MinimumReplicasAvailable" {
		t.Fatalf("details = %#v", details)
	}
}

func TestWorkloadsPodsUsesControllerSelector(t *testing.T) {
	t.Parallel()

	client := fake.NewSimpleClientset(
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "apps"},
			Spec: appsv1.DeploymentSpec{
				Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "api"}},
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "api-b", Namespace: "apps", Labels: map[string]string{"app": "api"}},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "api-a", Namespace: "apps", Labels: map[string]string{"app": "api"}},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "worker-a", Namespace: "apps", Labels: map[string]string{"app": "worker"}},
		},
	)

	pods, err := NewWorkloadServiceWithFactory(fakeFactory{client: client}).Pods(
		context.Background(),
		"apps",
		"Deployment",
		"api",
	)
	if err != nil {
		t.Fatalf("Pods() error = %v", err)
	}
	got := []string{pods[0].Name, pods[1].Name}
	want := []string{"api-a", "api-b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("pods = %#v, want %#v", got, want)
	}
}

func TestWorkloadsOwnerForPodResolvesControllerChains(t *testing.T) {
	t.Parallel()

	controller := true
	client := fake.NewSimpleClientset(
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "apps"},
			Status:     appsv1.DeploymentStatus{ReadyReplicas: 2},
		},
		&appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{
			Name: "api-abc", Namespace: "apps",
			OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", Name: "api", Controller: &controller}},
		}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{
			Name: "api-abc-123", Namespace: "apps",
			OwnerReferences: []metav1.OwnerReference{{Kind: "ReplicaSet", Name: "api-abc", Controller: &controller}},
		}},
		&batchv1.CronJob{ObjectMeta: metav1.ObjectMeta{Name: "cleanup", Namespace: "apps"}},
		&batchv1.Job{ObjectMeta: metav1.ObjectMeta{
			Name: "cleanup-123", Namespace: "apps",
			OwnerReferences: []metav1.OwnerReference{{Kind: "CronJob", Name: "cleanup", Controller: &controller}},
		}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{
			Name: "cleanup-123-abc", Namespace: "apps",
			OwnerReferences: []metav1.OwnerReference{{Kind: "Job", Name: "cleanup-123", Controller: &controller}},
		}},
	)
	service := NewWorkloadServiceWithFactory(fakeFactory{client: client})

	deployment, err := service.OwnerForPod(context.Background(), "apps", "api-abc-123")
	if err != nil || deployment.Kind != "Deployment" || deployment.Name != "api" {
		t.Fatalf("deployment owner = %#v, error = %v", deployment, err)
	}
	cronJob, err := service.OwnerForPod(context.Background(), "apps", "cleanup-123-abc")
	if err != nil || cronJob.Kind != "CronJob" || cronJob.Name != "cleanup" {
		t.Fatalf("CronJob owner = %#v, error = %v", cronJob, err)
	}
}

func TestWorkloadsDescribeCronJobIncludesOwnedJobs(t *testing.T) {
	t.Parallel()

	controller := true
	client := fake.NewSimpleClientset(
		&batchv1.CronJob{
			ObjectMeta: metav1.ObjectMeta{Name: "cleanup", Namespace: "apps"},
			Spec: batchv1.CronJobSpec{
				Schedule:          "0 * * * *",
				ConcurrencyPolicy: batchv1.ForbidConcurrent,
			},
		},
		&batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cleanup-scheduled",
				Namespace: "apps",
				OwnerReferences: []metav1.OwnerReference{{
					Kind: "CronJob", Name: "cleanup", Controller: &controller,
				}},
			},
			Status: batchv1.JobStatus{Active: 1},
		},
		&batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "unowned", Namespace: "apps"}},
		&batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "unrelated", Namespace: "apps"}},
	)

	details, err := NewWorkloadServiceWithFactory(fakeFactory{client: client}).DescribeCronJob(context.Background(), "apps", "cleanup")
	if err != nil {
		t.Fatalf("DescribeCronJob() error = %v", err)
	}
	if details.ConcurrencyPolicy != "Forbid" || len(details.Jobs) != 1 {
		t.Fatalf("details = %#v", details)
	}
	if details.Jobs[0].Name != "cleanup-scheduled" || details.Jobs[0].Status != "Running" {
		t.Fatalf("job = %#v", details.Jobs[0])
	}
}

func TestWorkloadsSetCronJobSuspendedUpdatesSpec(t *testing.T) {
	t.Parallel()

	client := fake.NewSimpleClientset(&batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{Name: "cleanup", Namespace: "apps"},
	})

	service := NewWorkloadServiceWithFactory(fakeFactory{client: client})
	if err := service.SetCronJobSuspended(context.Background(), "apps", "cleanup", true); err != nil {
		t.Fatalf("SetCronJobSuspended(true) error = %v", err)
	}
	cronJob, err := client.BatchV1().CronJobs("apps").Get(context.Background(), "cleanup", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if cronJob.Spec.Suspend == nil || !*cronJob.Spec.Suspend {
		t.Fatalf("Suspend = %#v, want true", cronJob.Spec.Suspend)
	}
	if err := service.SetCronJobSuspended(context.Background(), "apps", "cleanup", false); err != nil {
		t.Fatalf("SetCronJobSuspended(false) error = %v", err)
	}
	cronJob, err = client.BatchV1().CronJobs("apps").Get(context.Background(), "cleanup", metav1.GetOptions{})
	if err != nil || cronJob.Spec.Suspend == nil || *cronJob.Spec.Suspend {
		t.Fatalf("resumed CronJob = %#v, error = %v", cronJob.Spec.Suspend, err)
	}
}

func TestWorkloadsRolloutRestartUpdatesTemplateAnnotation(t *testing.T) {
	t.Parallel()

	client := fake.NewSimpleClientset(&appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "apps"},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{},
			Template: corev1.PodTemplateSpec{},
		},
	})
	service := NewWorkloadServiceWithFactory(fakeFactory{client: client})
	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	if err := service.RolloutRestart(context.Background(), "apps", "Deployment", "api"); err != nil {
		t.Fatalf("RolloutRestart() error = %v", err)
	}
	workload, err := client.AppsV1().Deployments("apps").Get(context.Background(), "api", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got := workload.Spec.Template.Annotations[restartedAtAnnotation]; got != now.Format(time.RFC3339) {
		t.Fatalf("annotation = %q", got)
	}
}

func TestWorkloadsRolloutProgressSummarizesDeploymentAndPods(t *testing.T) {
	t.Parallel()

	replicas := int32(3)
	restarted := "2026-06-14T10:00:00Z"
	client := fake.NewSimpleClientset(
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name: "api", Namespace: "apps", Generation: 4,
				Annotations: map[string]string{deploymentRevisionAnnotation: "7"},
			},
			Spec: appsv1.DeploymentSpec{
				Replicas: &replicas,
				Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "api"}},
				Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{restartedAtAnnotation: restarted},
				}},
			},
			Status: appsv1.DeploymentStatus{
				ObservedGeneration: 4, ReadyReplicas: 2, UpdatedReplicas: 2, AvailableReplicas: 2,
				Conditions: []appsv1.DeploymentCondition{{
					Type: appsv1.DeploymentProgressing, Status: corev1.ConditionTrue,
					Reason: "ReplicaSetUpdated", Message: "ReplicaSet api-new is progressing.",
				}},
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: "api-old", Namespace: "apps", Labels: map[string]string{"app": "api"},
				CreationTimestamp: metav1.NewTime(time.Date(2026, 6, 14, 9, 0, 0, 0, time.UTC)),
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: "api-new", Namespace: "apps", Labels: map[string]string{"app": "api"},
				CreationTimestamp: metav1.NewTime(time.Date(2026, 6, 14, 10, 0, 0, 0, time.UTC)),
			},
		},
	)

	progress, err := NewWorkloadServiceWithFactory(fakeFactory{client: client}).RolloutProgress(
		context.Background(), "apps", "Deployment", "api",
	)
	if err != nil {
		t.Fatalf("RolloutProgress() error = %v", err)
	}
	if progress.Complete || progress.Generation != 4 || progress.ObservedGeneration != 4 ||
		progress.Revision != "7" || progress.Status != "Progressing" || progress.Reason != "ReplicaSetUpdated" {
		t.Fatalf("progress = %#v", progress)
	}
	if progress.RestartedAt.Format(time.RFC3339) != restarted ||
		len(progress.Pods) != 2 || progress.Pods[0].Name != "api-new" {
		t.Fatalf("progress pods/restart = %#v", progress)
	}
}

func TestWorkloadsRolloutProgressDetectsCompletionAndStatefulSetRevisions(t *testing.T) {
	t.Parallel()

	replicas := int32(2)
	client := fake.NewSimpleClientset(&appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "apps", Generation: 3},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "db"}},
		},
		Status: appsv1.StatefulSetStatus{
			ObservedGeneration: 3, ReadyReplicas: 2, UpdatedReplicas: 2, AvailableReplicas: 2,
			CurrentRevision: "db-abc", UpdateRevision: "db-abc",
		},
	})

	progress, err := NewWorkloadServiceWithFactory(fakeFactory{client: client}).RolloutProgress(
		context.Background(), "apps", "StatefulSet", "db",
	)
	if err != nil || !progress.Complete || progress.Status != "Complete" ||
		progress.CurrentRevision != "db-abc" || progress.UpdateRevision != "db-abc" {
		t.Fatalf("RolloutProgress() = %#v, error = %v", progress, err)
	}
}
