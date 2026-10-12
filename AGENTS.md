# 项目约定(deepx 自动维护,启动时注入)

- deepx-code 仓库的 Git 工作流：上游 origin(itmisx/deepx-code) 不接收我们的提交，以后所有推送都在 fork(freakojc/deepx-code，即"我的仓库"/参考仓库) 中进行：独立分支推 fork + 合并到 fork 的 main，不再向 itmisx 推送或提 PR。
- 编译 deepx 的 Windows 产物时，输出文件名必须带后缀（如日期+改动特征），禁止覆盖旧版本产物（如 deepx-win-amd64-all-20261012-multi-instance.exe），便于区分各版本改动；本次的 deepx-win-amd64-all.exe 已改名为带后缀版本。
