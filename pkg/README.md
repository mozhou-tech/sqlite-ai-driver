# 存储驱动说明

本项目提供三个数据库存储驱动，它们都在配置的基础数据目录下使用各自的子目录进行数据存储。

## 目录结构

默认基础数据目录为 `./data`，目录结构如下：

```
./testdata/
├── data.db         # cayley-driver 的数据目录
├── data.db       # sqlite-driver 的共享数据库目录
└── data.db             # sqlite3-driver 的数据目录
```

