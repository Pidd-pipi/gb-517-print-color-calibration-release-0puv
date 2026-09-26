# 验收记录

初版验收日期：2026-08-22（Asia/Shanghai）

## 三位置校样门控（2026-09-26 更新）

本次变更把单一色差值改为操作侧、中间、传动侧三个读数，复核按最差位置判定：

- 新增后端单元/集成测试（`backend/internal/service/color_proof_gate_test.go`、`backend/internal/router/proof_gate_test.go`），覆盖：
  - 缺录位置时提交复核返回 422，批次停在 hold 并写明缺录原因；
  - 最差位置 ΔE 超过批次 `colorTolerance` 时不能接收（422），批次停在 hold，原因标明超限位置与数值；
  - 三位置均在范围内时按最差位置接收，批次回到 proofing 并清空滞留原因；
  - 接收后改动读数生成新判定版本、旧 accepted 结论留在历史（active=false），批次因“复核后读数被改动”停在 hold；
  - 驳回后重新测量属于正常纠偏，不记篡改；非读数字段编辑不产生新版本。
- 以 SQLite 模式（`DATABASE_DRIVER=sqlite`）手工 API 烟测走通缺录 → 超限 → 重测接收 → 接收后篡改全链路，批次版本链（`PrintRunRevision`）与审计日志均保留 hold/resume 原因和三位置读数。
- `scripts/validate.sh` 增加上述门控流程的 Compose 验收断言（缺录 422、超限 422、判定版本链 v1/v2/v3、批次自动回到 proofing）。
- 命令结果：`go test ./...`、`go vet ./...`、`gofmt -l`（无输出）、`go build ./...` 全部通过；前端 `npm run typecheck` 与 `npm run build` 通过。
- 规模：非测试 Go 代码 38 个文件、3814 行，符合 26-38 文件与 2700-3900 行范围。
- 说明：当前环境无 Docker，Compose 端到端脚本未在本机执行；容器化验收需在具备 Docker 的环境运行 `./scripts/validate.sh`。

## 静态与测试

以下命令均以退出码 0 完成：

```bash
cd backend
go test ./...
go test -race ./...
go vet ./...
go build ./...

cd ../frontend
npm run typecheck
npm run build

cd ..
docker compose config --quiet
```

- 路由集成测试覆盖 viewer 写入 403、operator 放行 403、reviewer 放行成功、已决记录更新 409，以及色彩配置/放行决定两条不可变修订链。
- 非测试 Go 代码为 38 个文件、3143 行，符合提示词的 26-38 文件与 2700-3900 行范围。

## 空卷 Compose 与 API

先执行 `docker compose down -v --remove-orphans`，随后以 `KEEP_RUNNING=1 ./scripts/validate.sh` 从空命名卷启动。MySQL、Redis、backend、frontend 均通过 healthcheck。

脚本实际验证：

- `/healthz`、前端首页、session、runtime、overview 和四组实体列表正常。
- 创建设备并推进状态后，审计总数和迁移计数同步增加。
- viewer 对写接口和审计接口均得到 403。
- operator 可采集校样并提交 review，但接收校样得到 403；reviewer 接收成功。
- operator 创建 draft 决定后直接 release 得到 403；reviewer 放行成功并生成 v2。
- 决定详情返回 v2/v1 两条修订，操作者分别为 reviewer/operator，请求 ID 分别为 `release-review-smoke`、`release-create-smoke`。

## 内置 Browser

只使用 Codex 内置 Browser 验收，没有调用外部 Chrome。

| 页面/场景 | 实测结果 |
|---|---|
| `/presses` | 列表、搜索区、新增确认框和 `ready -> setup` 状态推进正常 |
| `/runs` | 三条批次可见，`RunStateBadge` 显示待装版/印刷中/校样中 |
| `/proofs` | `ColorTable` 展示四条读数；详情复用同一组件并显示 v3 已接收校样 |
| `/release` | 放行依据读数、相关批次状态和决定详情正常；详情显示 v2/v1 完整版本链 |
| `/audit` | 审计列表显示操作者、迁移前后状态、实体和请求 ID |
| RBAC | viewer 无新增/推进/审计入口且直达 `/audit` 被重定向；operator 隐藏复核动作；reviewer 显示放行与审计入口 |
| 响应式 | 390x844 视口下导航、指标、工具栏和滚动表格无页面级横向溢出，`documentWidth == viewport == 390` |
| 控制台 | 全流程完成后 error/warning 日志为 0 |

最终交付前执行：

```bash
docker compose down -v --remove-orphans
```
