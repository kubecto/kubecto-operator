/*
Copyright 2022 The Kubernetes authors.

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
// +kubebuilder:docs-gen:collapse=Apache License

/*
We'll start out with some imports.  You'll see below that we'll need a few more imports
than those scaffolded for us.  We'll talk about each one when we use it.
*/
package controllers

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/robfig/cron"
	kbatch "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ref "k8s.io/client-go/tools/reference"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	batchv1 "tutorial.kubebuilder.io/project/api/v1"
)

/*
Next, we'll need a Clock, which will allow us to fake timing in our tests.
*/

// CronJobReconciler reconciles a CronJob object
type CronJobReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Clock
}

/*
我们将模拟这个时钟，让它在测试时更容易跳来跳去，
“真正的”时钟只会叫“现在”。
*/
type realClock struct{}

func (_ realClock) Now() time.Time { return time.Now() }

// clock知道如何获取当前时间。
// 它可以用来伪造测试时间。
type Clock interface {
	Now() time.Time
}

// +kubebuilder:docs-gen:collapse=Clock

/*
注意，我们还需要一些RBAC权限——因为我们正在创建和
现在管理job，我们需要这些job的权限，这意味着添加
还有几个[markers](/reference/markers/rbac.md)。
*/

//+kubebuilder:rbac:groups=batch.tutorial.kubebuilder.io,resources=cronjobs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=batch.tutorial.kubebuilder.io,resources=cronjobs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=batch.tutorial.kubebuilder.io,resources=cronjobs/finalizers,verbs=update
//+kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=batch,resources=jobs/status,verbs=get

/*
现在，我们进入控制器的核心。 -- reconciler 逻辑.
*/
var (
	scheduledTimeAnnotation = "batch.tutorial.kubebuilder.io/scheduled-at"
)

// Reconcile是kubernetes主要和解循环的一部分，其目的是
// 将集群的当前状态移向所需的状态。
// TODO(user):修改Reconcile函数以比较指定的状态
// CronJob对象根据实际的集群状态，然后
// 执行操作，使集群状态反映指定的状态用户。

//要了解更多细节，请查看这里的Reconcile and its Result:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.11.2/pkg/reconcile

func (r *CronJobReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	/*
          ### 1:按名称加载CronJob

          我们将使用我们的客户端获取CronJob。所有客户端方法都采用
          上下文(允许取消)作为它们的第一个参数，以及对象
          作为他们最后的疑问。Get有点特别，因为它需要一个
          [' NamespacedName '] (https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/client?tab=doc # ObjectKey)
          作为中间论点(大多数没有中间论点，我们会看到
          下文)。

          许多客户端方法最后也采用可变选项。

	*/
	var cronJob batchv1.CronJob
	if err := r.Get(ctx, req.NamespacedName, &cronJob); err != nil {
		log.Error(err, "unable to fetch CronJob")
		// we'll ignore not-found errors, since they can't be fixed by an immediate
		// requeue (we'll need to wait for a new notification), and we can get them
		// on deleted requests.
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	/*

        ### 2:列出所有活动的job，并更新状态

        要完全更新我们的状态，我们需要列出这个名称空间中属于这个CronJob的所有子job。
        与Get类似，我们可以使用List方法列出子job。注意，我们使用可变参数选项
        设置名称空间和字段匹配(这实际上是我们在下面设置的索引查找)。

	*/
	var childJobs kbatch.JobList
	if err := r.List(ctx, &childJobs, client.InNamespace(req.Namespace), client.MatchingFields{jobOwnerKey: req.Name}); err != nil {
		log.Error(err, "unable to list child Jobs")
		return ctrl.Result{}, err
	}

	/*

	          	<aside class="note">

         <h1>这个索引是关于什么的?< / h1 >

         调解器为该状态获取cronjob所拥有的所有作业。随着失业人数的增加，
         查找这些信息可能会变得相当缓慢，因为我们必须过滤所有的信息。为了更高效地查找，
         这些作业将根据控制器的名称在本地建立索引。将一个jobOwnerKey字段添加到
         缓存的工作对象。该键引用归属控制器，起索引的作用。后来在这
         文档中，我们将配置管理器来实际索引这个字段

         除了< / >

         一旦我们拥有了所有的工作，我们就把它们分成积极的，成功的，
         以及失败的作业，记录最近的运行情况这样我们就可以记录下来
         在状态。记住，状态应该可以从状态中重构出来
         世界之大，所以从地位上读一般不是一个好主意
         根对象。相反，您应该在每次运行时重构它。这就是我们
         这里所做的。

         我们可以使用状态来检查作业是否“完成”，以及它是成功还是失败
         条件。我们将把这个逻辑放到一个助手中，使我们的代码更清晰。

	*/

	// 查找工作的活动列表
	var activeJobs []*kbatch.Job
	var successfulJobs []*kbatch.Job
	var failedJobs []*kbatch.Job
	var mostRecentTime *time.Time // find the last run so we can update the status

	/*
如果一个job的“完成”或“失败”条件被标记为true，我们认为该作业“已完成”。
状态条件允许我们向其他对象添加可扩展的状态信息
人类和控制器可以检查完成情况和健康状况。
	*/
	isJobFinished := func(job *kbatch.Job) (bool, kbatch.JobConditionType) {
		for _, c := range job.Status.Conditions {
			if (c.Type == kbatch.JobComplete || c.Type == kbatch.JobFailed) && c.Status == corev1.ConditionTrue {
				return true, c.Type
			}
		}

		return false, ""
	}
	// +kubebuilder:docs-gen:collapse=isJobFinished

	/*
我们将使用一个助手从注释中提取计划时间
我们在创造就业时增加了。
	*/
	getScheduledTimeForJob := func(job *kbatch.Job) (*time.Time, error) {
		timeRaw := job.Annotations[scheduledTimeAnnotation]
		if len(timeRaw) == 0 {
			return nil, nil
		}

		timeParsed, err := time.Parse(time.RFC3339, timeRaw)
		if err != nil {
			return nil, err
		}
		return &timeParsed, nil
	}
	// +kubebuilder:docs-gen:collapse=getScheduledTimeForJob

	for i, job := range childJobs.Items {
		_, finishedType := isJobFinished(&job)
		switch finishedType {
		case "": // ongoing
			activeJobs = append(activeJobs, &childJobs.Items[i])
		case kbatch.JobFailed:
			failedJobs = append(failedJobs, &childJobs.Items[i])
		case kbatch.JobComplete:
			successfulJobs = append(successfulJobs, &childJobs.Items[i])
		}

//我们将启动时间存储在一个注释中，因此我们将重新构造它激活job本身。
		scheduledTimeForJob, err := getScheduledTimeForJob(&job)
		if err != nil {
			log.Error(err, "unable to parse schedule time for child job", "job", &job)
			continue
		}
		if scheduledTimeForJob != nil {
			if mostRecentTime == nil {
				mostRecentTime = scheduledTimeForJob
			} else if mostRecentTime.Before(*scheduledTimeForJob) {
				mostRecentTime = scheduledTimeForJob
			}
		}
	}

	if mostRecentTime != nil {
		cronJob.Status.LastScheduleTime = &metav1.Time{Time: *mostRecentTime}
	} else {
		cronJob.Status.LastScheduleTime = nil
	}
	cronJob.Status.Active = nil
	for _, activeJob := range activeJobs {
		jobRef, err := ref.GetReference(r.Scheme, activeJob)
		if err != nil {
			log.Error(err, "unable to make reference to active job", "job", activeJob)
			continue
		}
		cronJob.Status.Active = append(cronJob.Status.Active, *jobRef)
	}

	/*
在这里，我们将记录在稍高的日志级别上观察到的job数量，进行调试。注意，我们不使用格式字符串，而是使用固定消息，并附加附加信息的键值对。这使它更容易
过滤和查询日志行。
	*/
	log.V(1).Info("job count", "active jobs", len(activeJobs), "successful jobs", len(successfulJobs), "failed jobs", len(failedJobs))

	/*
使用我们收集到的日期，我们将更新CRD的状态。和以前一样，我们利用客户。来专门更新状态子资源，我们将使用客户端的“状态”部分，与“更新”方法。

status子资源忽略spec的更改，因此不太可能发生冲突与任何其他更新，可以有单独的权限。
	*/
	if err := r.Status().Update(ctx, &cronJob); err != nil {
		log.Error(err, "unable to update CronJob status")
		return ctrl.Result{}, err
	}

	/*
一旦我们更新了状态，我们就可以继续确保状态这个世界符合我们在规格中想要的东西。

### 3:根据历史限制清理旧job

首先，我们要清理过去的job，这样我们就不会留下太多的谎言周围。
	*/

//注意:删除这些是“best effort”——如果我们在特定的一个上失败了，
//我们不会仅仅为了完成删除而请求队列。
	if cronJob.Spec.FailedJobsHistoryLimit != nil {
		sort.Slice(failedJobs, func(i, j int) bool {
			if failedJobs[i].Status.StartTime == nil {
				return failedJobs[j].Status.StartTime != nil
			}
			return failedJobs[i].Status.StartTime.Before(failedJobs[j].Status.StartTime)
		})
		for i, job := range failedJobs {
			if int32(i) >= int32(len(failedJobs))-*cronJob.Spec.FailedJobsHistoryLimit {
				break
			}
			if err := r.Delete(ctx, job, client.PropagationPolicy(metav1.DeletePropagationBackground)); client.IgnoreNotFound(err) != nil {
				log.Error(err, "unable to delete old failed job", "job", job)
			} else {
				log.V(0).Info("deleted old failed job", "job", job)
			}
		}
	}

	if cronJob.Spec.SuccessfulJobsHistoryLimit != nil {
		sort.Slice(successfulJobs, func(i, j int) bool {
			if successfulJobs[i].Status.StartTime == nil {
				return successfulJobs[j].Status.StartTime != nil
			}
			return successfulJobs[i].Status.StartTime.Before(successfulJobs[j].Status.StartTime)
		})
		for i, job := range successfulJobs {
			if int32(i) >= int32(len(successfulJobs))-*cronJob.Spec.SuccessfulJobsHistoryLimit {
				break
			}
			if err := r.Delete(ctx, job, client.PropagationPolicy(metav1.DeletePropagationBackground)); (err) != nil {
				log.Error(err, "unable to delete old successful job", "job", job)
			} else {
				log.V(0).Info("deleted old successful job", "job", job)
			}
		}
	}

	/* 
### 4:检查我们是否被暂停

如果这个对象被挂起，我们就不想运行任何job，所以我们现在就停止。
如果我们正在运行的job出现了问题，而我们想要这样做的话，这是很有用的
在不删除对象的情况下，Pause运行来调查或处理集群。
	*/

	if cronJob.Spec.Suspend != nil && *cronJob.Spec.Suspend {
		log.V(1).Info("cronjob suspended, skipping")
		return ctrl.Result{}, nil
	}

	/*
### 5:获得下一次计划运行

如果我们没有暂停，我们将需要计算下一次计划运行，以及是否
或者不是，我们还有一桩案子还没处理。
	*/

	/*
我们将使用有用的cron库计算下一个预定时间。我们将从上次运行或创建开始计算适当的时间
如果我们找不到最后一班车，就会被解雇。如果错过的次数太多而我们又没有截止日期，我们会保释，这样我们就不会在控制器重启或楔子上造成问题。

否则，我们将返回错过的运行(我们将使用最新的)，还有下一次，这样我们就能知道什么时候该再次和解了。
	*/
	getNextSchedule := func(cronJob *batchv1.CronJob, now time.Time) (lastMissed time.Time, next time.Time, err error) {
		sched, err := cron.ParseStandard(cronJob.Spec.Schedule)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("Unparseable schedule %q: %v", cronJob.Spec.Schedule, err)
		}

//为了优化的目的，作弊一点，从我们上次观察到的运行时开始
//我们可以在这里重新构造它，但没有什么意义，因为我们已经
//刚刚更新。
		var earliestTime time.Time
		if cronJob.Status.LastScheduleTime != nil {
			earliestTime = cronJob.Status.LastScheduleTime.Time
		} else {
			earliestTime = cronJob.ObjectMeta.CreationTimestamp.Time
		}
		if cronJob.Spec.StartingDeadlineSeconds != nil {
			// 控制器不会安排任何低于这个点的事情
			schedulingDeadline := now.Add(-time.Second * time.Duration(*cronJob.Spec.StartingDeadlineSeconds))

			if schedulingDeadline.After(earliestTime) {
				earliestTime = schedulingDeadline
			}
		}
		if earliestTime.After(now) {
			return time.Time{}, sched.Next(now), nil
		}

		starts := 0
		for t := sched.Next(earliestTime); !t.After(now); t = sched.Next(t) {
			lastMissed = t
/*
一个对象可能会错过几次启动。例如，如果控制器在周五下午5:01卡住了，那时所有人都回家了，有人在周二早上进来，发现了问题并重新启动控制器，那么所有的小时作业，其中超过80个是一个小时调度作业，应该在没有进一步干预的情况下开始运行(如果schedulejob允许并发和延迟启动)。但是，如果某个地方有bug，或者控制器的服务器或apiservers(用于设置creationTimestamp)上的时钟不正确，那么可能会有很多错过的开始时间(可能是几十年或更久)，这会耗尽控制器的所有CPU和内存。在这种情况下，我们不想列出所有错过的开始时间。
*/
			starts++
			if starts > 100 {
				// 我们无法得到最近的时间，所以只返回一个空的切片
				return time.Time{}, time.Time{}, fmt.Errorf("Too many missed start times (> 100). Set or decrease .spec.startingDeadlineSeconds or check clock skew.")
			}
		}
		return lastMissed, sched.Next(now), nil
	}
	// +kubebuilder:docs-gen:collapse=getNextSchedule

//找出我们需要在下一次创造job机会的时间点(或遗漏的时间点)。
	missedRun, nextRun, err := getNextSchedule(&cronJob, r.Now())
	if err != nil {
		log.Error(err, "unable to figure out CronJob schedule")
//我们并不关心是否需要排队，直到我们得到一个更新来修复时间表，所以不要返回错误
		return ctrl.Result{}, nil
	}

	/*
我们将准备最终请求，等待到下一个job，然后进行计算
如果我们真的要逃跑，就出去。
	*/
	scheduledResult := ctrl.Result{RequeueAfter: nextRun.Sub(r.Now())} // save this so we can re-use it elsewhere
	log = log.WithValues("now", r.Now(), "next run", nextRun)

	/*
### 6:运行一个新job，如果它在计划中，而不是超过最后期限，并且没有被我们的并发策略阻塞

如果我们错过了一次运行，而我们仍然在最后期限内启动它，我们将需要运行一个job。
	*/
	if missedRun.IsZero() {
		log.V(1).Info("no upcoming scheduled times, sleeping until next")
		return scheduledResult, nil
	}

	// 确保我们还来得及出发
	log = log.WithValues("current run", missedRun)
	tooLate := false
	if cronJob.Spec.StartingDeadlineSeconds != nil {
		tooLate = missedRun.Add(time.Duration(*cronJob.Spec.StartingDeadlineSeconds) * time.Second).Before(r.Now())
	}
	if tooLate {
		log.V(1).Info("missed starting deadline for last run, sleeping till next")
		// TODO(directxman12): events
		return scheduledResult, nil
	}

	/*
如果我们真的要运行一个job，我们需要等待现有的job完成，
替换现有的，或者添加新的。如果我们的信息过期了
为了缓存延迟，当我们获得最新信息时，我们会得到一个requeue。
	*/
//找出如何运行这个job——并发策略可能会阻止我们运行
//多个在同一时间…
	if cronJob.Spec.ConcurrencyPolicy == batchv1.ForbidConcurrent && len(activeJobs) > 0 {
		log.V(1).Info("concurrency policy blocks concurrent runs, skipping", "num active", len(activeJobs))
		return scheduledResult, nil
	}

//.．.或者指示我们替换现有的……
	if cronJob.Spec.ConcurrencyPolicy == batchv1.ReplaceConcurrent {
		for _, activeJob := range activeJobs {
			// 我们不关心作业是否已经被删除
			if err := r.Delete(ctx, activeJob, client.PropagationPolicy(metav1.DeletePropagationBackground)); client.IgnoreNotFound(err) != nil {
				log.Error(err, "unable to delete active job", "job", activeJob)
				return ctrl.Result{}, err
			}
		}
	}

	/*
一旦我们弄清楚如何处理现有的job，我们就会创造出我们想要的job
	*/

	/*
我们需要基于CronJob的模板构建一个job。我们会复制说明书的
从模板中复制一些基本的对象元。

然后，我们将设置“计划时间”注释，以便我们可以重构我们的
' LastScheduleTime '字段每一次和解。

最后，我们需要设置一个所有者引用。这允许Kubernetes垃圾收集器
当我们删除CronJob时清理job，并允许控制器运行时计算
当给定的job发生变化(添加、删除、完成等)时，需要协调哪个cronjob。
	*/
	constructJobForCronJob := func(cronJob *batchv1.CronJob, scheduledTime time.Time) (*kbatch.Job, error) {
//我们希望给定名义开始时间的job名称具有确定的名称，以避免创建两次相同的job
		name := fmt.Sprintf("%s-%d", cronJob.Name, scheduledTime.Unix())

		job := &kbatch.Job{
			ObjectMeta: metav1.ObjectMeta{
				Labels:      make(map[string]string),
				Annotations: make(map[string]string),
				Name:        name,
				Namespace:   cronJob.Namespace,
			},
			Spec: *cronJob.Spec.JobTemplate.Spec.DeepCopy(),
		}
		for k, v := range cronJob.Spec.JobTemplate.Annotations {
			job.Annotations[k] = v
		}
		job.Annotations[scheduledTimeAnnotation] = scheduledTime.Format(time.RFC3339)
		for k, v := range cronJob.Spec.JobTemplate.Labels {
			job.Labels[k] = v
		}
		if err := ctrl.SetControllerReference(cronJob, job, r.Scheme); err != nil {
			return nil, err
		}

		return job, nil
	}
	// +kubebuilder:docs-gen:collapse=constructJobForCronJob

	//  make job...
	job, err := constructJobForCronJob(&cronJob, missedRun)
	if err != nil {
		log.Error(err, "unable to construct job from template")
		// 在我们修改说明书之前不要排队
		return scheduledResult, nil
	}

	// ...并在集群上创建它
	if err := r.Create(ctx, job); err != nil {
		log.Error(err, "unable to create Job for CronJob", "job", job)
		return ctrl.Result{}, err
	}

	log.V(1).Info("created Job for CronJob run", "job", job)

	/*
### 7:当我们看到正在运行的job或者是下一次计划运行的时间时，请Requeue

最后，我们将返回我们在上面准备的结果，它表示我们想要requeue
当我们需要下一次行动的时候。这被认为是一个最大的期限
其他的变化，比如我们的工作开始或结束，我们被修改，等等，我们可能
再次协调。
	*/
	// 一旦看到正在运行的作业，我们将重新排队，并更新状态返回scheduledResult,零
	return scheduledResult, nil
}

/*
# # #设置

最后，我们将更新设置。为了让我们的调解人能够迅速
查乔布斯的主人，我们需要索引。我们声明一个索引键
我们可以稍后使用客户端作为伪字段名，然后描述如何
从Job对象中提取索引值。索引器将自动获取
为我们关心名称空间，所以我们只需要提取所有者的名字，如果Job有
一个计划的所有者。

另外，我们将通知管理器这个控制器拥有一些job，因此它
将自动调用在基础CronJob上的和解，当一个工作变化，是
删除等。
*/
var (
	jobOwnerKey = ".metadata.controller"
	apiGVStr    = batchv1.GroupVersion.String()
)

func (r *CronJobReconciler) SetupWithManager(mgr ctrl.Manager) error {
//设置一个真正的时钟，因为我们不是在测试
	if r.Clock == nil {
		r.Clock = realClock{}
	}

	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &kbatch.Job{}, jobOwnerKey, func(rawObj client.Object) []string {
		// 获取作业对象，提取所有者…
		job := rawObj.(*kbatch.Job)
		owner := metav1.GetControllerOf(job)
		if owner == nil {
			return nil
		}
		// ...make CronJob...
		if owner.APIVersion != apiGVStr || owner.Kind != "CronJob" {
			return nil
		}

		// ...如果是，返回它
		return []string{owner.Name}
	}); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&batchv1.CronJob{}).
		Owns(&kbatch.Job{}).
		Complete(r)
}
