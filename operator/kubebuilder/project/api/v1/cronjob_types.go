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
 */
package v1

/*
 */
import (
	batchv1beta1 "k8s.io/api/batch/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// 编辑这个文件!这是你自己的脚手架!
// 注意:json标签是必需的。添加的任何新字段都必须具有要序列化的字段的json标记。

// +kubebuilder:docs-gen:collapse=Imports

/*
首先，让我们看看我们的规范。正如我们之前讨论的，规范是成立的
*期望状态*，所以控制器的任何“输入”都到这里。

从根本上说，一个CronJob需要以下几点:

    - 一个时间表(CronJob中的*cron*)
    - 要运行的Job的模板* *工作计划)

我们还需要一些额外的功能，让用户更轻松:

    - 开始工作的最后期限(如果我们错过了这个期限，我们就等到下一个预定时间)
    - 如果多个作业同时运行(我们等待吗?阻止旧的那个?运行两个?)
    - 一种暂停CronJob运行的方法，以防出现问题
    - 对之前执行的job任务的限制

记住，既然我们从来没有读过自己的状态，我们需要一些其他的方法跟踪某个作业是否已运行。我们至少可以用一项旧工作来做这一点。

我们将使用几个标记(' // +comment ')来指定额外的元数据。这些
将被[controller-tools](https://github.com/kubernetes-sigs/controller-tools)在生成我们的CRD清单时使用。
正如我们将看到的，控制器工具也将使用GoDoc来形成描述
的字段。
*/

// CronJobSpec定义了CronJob的期望状态
type CronJobSpec struct {
	//+kubebuilder:validation:MinLength=0

	// Cron格式的时间表，请参见https://en.wikipedia.org/wiki/Cron。
	Schedule string `json:"schedule"`

	//+kubebuilder:validation:Minimum=0

	// 可选的截止日期，单位为秒
	// 时间。未执行的任务将被算作失败的任务。
	// +可选
	StartingDeadlineSeconds *int64 `json:"startingDeadlineSeconds,omitempty"`

	// 指定如何处理Job的并发执行。
	// 有效的值是:
	// - "Allow" (default):允许CronJobs并行运行;
	// - "Forbid":禁止并发运行，如果上一个运行还没有完成，则跳过下一个运行;
	// - "Replace":取消当前运行的作业，并替换为新的作业
	// +可选
	ConcurrencyPolicy ConcurrencyPolicy `json:"concurrencyPolicy,omitempty"`

	// 这个标志告诉控制器暂停后续的执行，它做到了
	// 不应用于已经开始的执行。默认值为false。
	// +可选
	Suspend *bool `json:"suspend,omitempty"`

	// 执行CronJob时要创建的job。
	JobTemplate batchv1beta1.JobTemplateSpec `json:"jobTemplate"`

	//+kubebuilder:validation:Minimum=0

	// 保留成功完成的工作的数量。
	// 这是一个指针，区分显式零和未指定。
	// +可选
	SuccessfulJobsHistoryLimit *int32 `json:"successfulJobsHistoryLimit,omitempty"`

	//+kubebuilder:validation:Minimum=0

	// 保留失败的已完成任务数。
	// 这是一个指针，区分显式零和未指定。
	// +可选
	FailedJobsHistoryLimit *int32 `json:"failedJobsHistoryLimit,omitempty"`
}

/*
我们定义了一个自定义类型来保存并发策略。它实际上是
表面上只是一个字符串，但类型提供了额外的文档，
允许我们将验证附加到类型而不是字段，
使验证更容易重复使用。
*/

// ConcurrencyPolicy描述了任务将如何被处理。
// 以下并发策略只能指定其中之一。
// 如果不指定以下策略，则使用默认策略
// AllowConcurrent。
// +kubebuilder:validation:Enum=Allow;Forbid;Replace
type ConcurrencyPolicy string

const (
	// AllowConcurrent允许CronJobs并发运行
	AllowConcurrent ConcurrencyPolicy = "Allow"

	// ForbidConcurrent禁止并发运行，如果前一次还没有完成，则跳过下一次运行。
	ForbidConcurrent ConcurrencyPolicy = "Forbid"

	// ReplaceConcurrent取消当前正在运行的作业，并用一个新的作业替换它。
	ReplaceConcurrent ConcurrencyPolicy = "Replace"
)

/*
接下来，让我们设计状态，它保存观察到的状态。它包含任何信息
我们希望用户或其他控制器能够轻松获取。

我们将保存一个活动运行作业的列表，以及上次成功运行的时间
我们的工作。注意，我们使用' metav1。而不是“时间”。如上所述。
*/

// CronJobStatus定义了CronJob的观察状态
type CronJobStatus struct {

	// 插入附加的状态字段-定义集群的观察状态
	// 重要提示:修改此文件后，执行"make"命令重新生成代码

	// 指向当前运行的作业的指针列表。
	// +可选? ?
	Active []corev1.ObjectReference `json:"active,omitempty"`

	// 上次任务调度成功的时间。
	// +可选
	LastScheduleTime *metav1.Time `json:"lastScheduleTime,omitempty"`
}

/*
最后，我们有我们已经讨论过的其他样板文件。
如前所述，我们不需要更改这个，只需要标记那个
我们需要一个状态子资源，这样我们的行为就像内置的kubernetes类型。
*/

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// CronJob是cronjobs API的架构
type CronJob struct {
	/*
	 */
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CronJobSpec   `json:"spec,omitempty"`
	Status CronJobStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// CronJobList包含CronJob的列表
type CronJobList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CronJob `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CronJob{}, &CronJobList{})
}

//+kubebuilder:docs-gen:collapse=Root Object Definitions
