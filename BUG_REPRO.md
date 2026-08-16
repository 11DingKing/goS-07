# Bug Reproduction

## 包的性质

当前 test_model_fix 保存的是被测模型修复后的结果源码，不是初始含 Bug 源码。要复现原始缺陷，必须检出下面固定的 parent SHA；不要在当前修复结果源码上期待重新出现修复前失败。生成系统使用的可信验证补丁和完整验证日志仅在本地留存，不提交到结果分支。

## 问题现象

服务在 K8s 里滚动更新一直卡住，每个 Pod 都要等到 terminationGracePeriodSeconds 到点被 SIGKILL 才消失，
滚动更新因此非常慢，还偶发两个副本同时在跑。

本地能稳定复现：
  go build -o ejina-microgrid . && ./ejina-microgrid
  然后另开一个终端 kill -TERM <pid>
日志里能看到 shutting down...，HTTP 端口也确实不再接受新连接了，但进程就是不退出，一直挂着，最后只能 kill -9。
kill -INT（Ctrl-C）也一样，进程不肯结束。

期望行为：收到 SIGINT/SIGTERM 之后，HTTP 服务优雅关闭、后台周期任务全部停下来，进程在几秒内自己正常退出，退出码正常。
同时后台任务在服务正常运行期间必须照常按各自间隔执行，不能因为这个改动被提前停掉。

请修复，修复后 go test ./... -count=1 要全绿。

## 含 Bug 版本

- 仓库：11DingKing/goS-07
- 仓库地址：https://github.com/11DingKing/goS-07.git
- parent SHA：0932de46ffed67b7cc7d25cc95b887921c756b7e

## 复现步骤

```bash
git clone -- https://github.com/11DingKing/goS-07.git bug-repro
cd bug-repro
git checkout --detach 0932de46ffed67b7cc7d25cc95b887921c756b7e
go test ./internal/job/ -run "^TestSchedulerStartStop$" -count=1 -timeout=120s
```

## 双架构完整错误信息

### linux/amd64

- 容器内复现预期退出码：1
- 容器内复现实际退出码：1

stdout：

```text
$ go test ./internal/job/ -run "^TestSchedulerStartStop$" -count=1 -timeout=120s
2026/08/16 23:25:34 [job:health] started (interval=1s)
2026/08/16 23:25:34 [job:snapshot] started (interval=5s)
2026/08/16 23:25:34 [job:grid-transition] started (interval=1s)
2026/08/16 23:25:34 [job:escalation] started (interval=30s)
2026/08/16 23:25:35 [job:health] controller failover triggered, backup took over with snapshot replay
--- FAIL: TestSchedulerStartStop (2.01s)
    job_test.go:54: scheduler did not stop within 2s
FAIL
FAIL	ejina-microgrid/internal/job	2.032s
FAIL

```

stderr：

```text
(empty)
```

### linux/arm64

- 容器内复现预期退出码：1
- 容器内复现实际退出码：1

stdout：

```text
$ go test ./internal/job/ -run "^TestSchedulerStartStop$" -count=1 -timeout=120s
2026/08/16 23:26:12 [job:escalation] started (interval=30s)
2026/08/16 23:26:12 [job:health] started (interval=1s)
2026/08/16 23:26:12 [job:snapshot] started (interval=5s)
2026/08/16 23:26:12 [job:grid-transition] started (interval=1s)
2026/08/16 23:26:13 [job:health] controller failover triggered, backup took over with snapshot replay
--- FAIL: TestSchedulerStartStop (2.01s)
    job_test.go:54: scheduler did not stop within 2s
FAIL
FAIL	ejina-microgrid/internal/job	2.008s
FAIL

```

stderr：

```text
(empty)
```

## 通过条件

定向复现命令在修复前失败、修复后通过（exit 0），包含 -race 版本。
go test ./... -count=1 -timeout=300s 与 go test -race ./... -count=1 -timeout=600s 全绿。
go build ./...、go vet ./... 通过，gofmt -l . 无输出。
编译后的二进制收到 SIGTERM 后必须自行退出，不需要 SIGKILL。
后台周期任务在未取消时仍按各自 interval 正常执行，不得改成一次性任务或直接不启动。
只修改生产代码；不得新增、删除、改写或跳过任何 *_test.go 中的测试与断言。
linux/amd64 与 linux/arm64 两个架构上结果一致。
