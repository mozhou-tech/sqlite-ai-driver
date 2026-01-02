package cayley_driver

import "context"

// Triple 表示图数据库中的三元组（subject-predicate-object）
type Triple struct {
	Subject   string
	Predicate string
	Object    string
}

// Graph 定义图数据库的接口
type Graph interface {
	// Link 创建一条从 subject 到 object 的边，边的类型为 predicate
	Link(ctx context.Context, subject, predicate, object string) error

	// Unlink 删除一条边
	Unlink(ctx context.Context, subject, predicate, object string) error

	// GetNeighbors 获取指定节点的邻居节点
	// node: 节点ID
	// predicate: 边的类型，如果为空则返回所有类型的邻居
	GetNeighbors(ctx context.Context, node, predicate string) ([]string, error)

	// GetInNeighbors 获取指向指定节点的邻居节点（入边）
	GetInNeighbors(ctx context.Context, node, predicate string) ([]string, error)

	// Query 返回查询构建器，支持类似 Gremlin 的查询语法
	Query() GraphQuery

	// FindPath 查找从 from 到 to 的路径
	// maxDepth: 最大深度
	// predicate: 边的类型，如果为空则允许所有类型的边
	FindPath(ctx context.Context, from, to string, maxDepth int, predicate string) ([][]string, error)

	// AllTriples 获取图中所有的三元组
	AllTriples(ctx context.Context) ([]Triple, error)

	// Close 关闭图数据库连接
	Close() error
}

// GraphQuery 定义图查询构建器的接口
type GraphQuery interface {
	// V 选择指定的节点
	V(node string) GraphQuery

	// Out 沿着指定的边类型向外遍历
	Out(predicate string) GraphQuery

	// In 沿着指定的边类型向内遍历
	In(predicate string) GraphQuery

	// Both 沿着所有边类型双向遍历（出边和入边）
	Both() GraphQuery

	// All 执行查询并返回所有结果
	All(ctx context.Context) ([]Triple, error)

	// Values 执行查询并返回所有节点值
	Values(ctx context.Context) ([]string, error)
}
