# 在线更新

管理后台的“在线更新”只部署 GitHub 中已发布的最新稳定 Release。它不会接受浏览器传来的分支、命令或下载地址，也不会把 Docker socket 挂载进应用容器。

## 一次性安装

以下命令在部署服务器执行，假设仓库位于 `/opt/image2api`：

```bash
cd /opt/image2api
sudo ./ops/updater/install.sh /opt/image2api
sudoedit /etc/image2api/updater.env
```

安装器会仅将指定部署目录加入 Git 的系统级安全目录白名单，使拥有 Docker 权限的更新器能够操作由普通部署用户维护的仓库。

在 `/etc/image2api/updater.env` 中设置随机密钥，并确认仓库与 Compose 文件：

```dotenv
UPDATER_TOKEN=<粘贴 `openssl rand -hex 32` 的输出>
IMAGE2API_REPO=/opt/image2api
IMAGE2API_GITHUB_REPO=damian2848/image2api
IMAGE2API_COMPOSE_FILES=docker-compose.yml,docker-compose.prod.yml
```

把同一个密钥写入部署目录的 `.env`，供 `backend` 容器连接仅监听本机回环地址的更新器：

```dotenv
UPDATER_URL=http://host.docker.internal:7070
UPDATER_TOKEN=<与 /etc/image2api/updater.env 相同的随机值>
```

然后启动更新器，并重建一次应用容器使环境变量生效：

```bash
sudo systemctl restart image2api-updater
sudo systemctl status image2api-updater
docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d --build
```

若只使用 `docker-compose.yml`，把 `IMAGE2API_COMPOSE_FILES` 改为 `docker-compose.yml`，并在最后一条命令中也省略生产覆盖文件。

## 发布与更新

从准备发布的提交创建并推送语义化 tag。仓库中的 GitHub Actions 会据此创建 GitHub Release：

```bash
git tag -a v0.2.0 -m "v0.2.0"
git push origin v0.2.0
```

Release 创建完成后，以管理员登录，打开“在线更新”，检查版本并确认“更新并重启”。更新器按顺序执行：

1. 确认工作树没有已跟踪的本地修改。
2. 获取 GitHub Latest Release 对应的 tag，并切换到该不可变版本。
3. 仅重建并重启 `backend`、`web` 容器。

PostgreSQL、Redis、RustFS 与生成文件卷不会被删除或重建。若镜像构建失败，更新器会恢复仓库源码到开始更新前的提交，现有容器继续运行。可用 `sudo journalctl -u image2api-updater -f` 查看宿主机更新日志。
