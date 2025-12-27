package main

import (
	"context"
	"fmt"
	"log"

	cayley_driver "github.com/mozhou-tech/sqlite-ai-driver/pkg/cayley-driver"
)

func main() {
	ctx := context.Background()

	// 创建图数据库实例
	// 数据库路径支持相对路径（会自动构建到 data/cayley/ 目录）
	// 也可以使用绝对路径，如："/path/to/graph.db"
	// 或者使用环境变量 DATA_DIR 指定数据目录
	graph, err := cayley_driver.NewGraph("cayley_example.db")
	if err != nil {
		log.Fatalf("创建图数据库失败: %v", err)
	}
	defer graph.Close()

	fmt.Println("✅ 成功创建图数据库实例")

	// ========== 示例 1: 创建社交网络关系 ==========
	fmt.Println("\n📝 示例 1: 创建社交网络关系...")

	// 创建关注关系
	if err := graph.Link(ctx, "alice", "follows", "bob"); err != nil {
		log.Fatalf("创建关系失败: %v", err)
	}
	if err := graph.Link(ctx, "bob", "follows", "charlie"); err != nil {
		log.Fatalf("创建关系失败: %v", err)
	}
	if err := graph.Link(ctx, "charlie", "follows", "david"); err != nil {
		log.Fatalf("创建关系失败: %v", err)
	}
	if err := graph.Link(ctx, "alice", "follows", "charlie"); err != nil {
		log.Fatalf("创建关系失败: %v", err)
	}
	if err := graph.Link(ctx, "bob", "follows", "david"); err != nil {
		log.Fatalf("创建关系失败: %v", err)
	}

	fmt.Println("✅ 创建了以下关注关系:")
	fmt.Println("  - alice -> follows -> bob")
	fmt.Println("  - bob -> follows -> charlie")
	fmt.Println("  - charlie -> follows -> david")
	fmt.Println("  - alice -> follows -> charlie")
	fmt.Println("  - bob -> follows -> david")

	// ========== 示例 2: 查询邻居节点 ==========
	fmt.Println("\n🔍 示例 2: 查询邻居节点...")

	// 查询 alice 关注的所有人
	neighbors, err := graph.GetNeighbors(ctx, "alice", "follows")
	if err != nil {
		log.Fatalf("查询邻居失败: %v", err)
	}
	fmt.Printf("✅ alice 关注的人: %v\n", neighbors)

	// 查询关注 bob 的所有人（入边）
	inNeighbors, err := graph.GetInNeighbors(ctx, "bob", "follows")
	if err != nil {
		log.Fatalf("查询入边邻居失败: %v", err)
	}
	fmt.Printf("✅ 关注 bob 的人: %v\n", inNeighbors)

	// 查询 alice 的所有邻居（不指定边的类型）
	allNeighbors, err := graph.GetNeighbors(ctx, "alice", "")
	if err != nil {
		log.Fatalf("查询所有邻居失败: %v", err)
	}
	fmt.Printf("✅ alice 的所有邻居: %v\n", allNeighbors)

	// ========== 示例 3: 使用查询 API（类似 Gremlin） ==========
	fmt.Println("\n🔍 示例 3: 使用查询 API...")

	// 查询 alice 关注的所有人（返回三元组）
	query := graph.Query()
	results, err := query.V("alice").Out("follows").All(ctx)
	if err != nil {
		log.Fatalf("查询失败: %v", err)
	}
	fmt.Println("✅ 查询 alice 关注的所有人（三元组）:")
	for _, triple := range results {
		fmt.Printf("  - %s -> %s -> %s\n", triple.Subject, triple.Predicate, triple.Object)
	}

	// 查询 alice 关注的所有人（只返回节点值）
	values, err := query.V("alice").Out("follows").Values(ctx)
	if err != nil {
		log.Fatalf("查询失败: %v", err)
	}
	fmt.Printf("✅ alice 关注的人（节点值）: %v\n", values)

	// 查询关注 bob 的所有人（入边查询）
	inValues, err := query.V("bob").In("follows").Values(ctx)
	if err != nil {
		log.Fatalf("查询失败: %v", err)
	}
	fmt.Printf("✅ 关注 bob 的人（节点值）: %v\n", inValues)

	// ========== 示例 4: 链式查询 ==========
	fmt.Println("\n🔍 示例 4: 链式查询（多步遍历）...")

	// 查询 alice 经过两步关注的人（alice -> follows -> X -> follows -> Y）
	chainValues, err := query.V("alice").Out("follows").Out("follows").Values(ctx)
	if err != nil {
		log.Fatalf("链式查询失败: %v", err)
	}
	fmt.Printf("✅ alice 经过两步关注的人: %v\n", chainValues)

	// 查询 alice 经过两步关注的人（返回三元组）
	chainResults, err := query.V("alice").Out("follows").Out("follows").All(ctx)
	if err != nil {
		log.Fatalf("链式查询失败: %v", err)
	}
	fmt.Println("✅ alice 经过两步关注的人（三元组）:")
	for _, triple := range chainResults {
		fmt.Printf("  - %s -> %s -> %s\n", triple.Subject, triple.Predicate, triple.Object)
	}

	// ========== 示例 5: 路径查找 ==========
	fmt.Println("\n🔍 示例 5: 路径查找...")

	// 查找从 alice 到 david 的所有路径（最大深度 5）
	paths, err := graph.FindPath(ctx, "alice", "david", 5, "follows")
	if err != nil {
		log.Fatalf("路径查找失败: %v", err)
	}
	fmt.Printf("✅ 从 alice 到 david 的路径（共 %d 条）:\n", len(paths))
	for i, path := range paths {
		fmt.Printf("  路径 %d: %v\n", i+1, path)
	}

	// ========== 示例 6: 创建多种关系类型 ==========
	fmt.Println("\n📝 示例 6: 创建多种关系类型...")

	// 创建点赞关系
	if err := graph.Link(ctx, "alice", "likes", "bob"); err != nil {
		log.Fatalf("创建关系失败: %v", err)
	}
	if err := graph.Link(ctx, "bob", "likes", "charlie"); err != nil {
		log.Fatalf("创建关系失败: %v", err)
	}

	// 创建朋友关系
	if err := graph.Link(ctx, "alice", "friend", "charlie"); err != nil {
		log.Fatalf("创建关系失败: %v", err)
	}
	if err := graph.Link(ctx, "charlie", "friend", "alice"); err != nil {
		log.Fatalf("创建关系失败: %v", err)
	}

	fmt.Println("✅ 创建了多种关系类型:")
	fmt.Println("  - alice -> likes -> bob")
	fmt.Println("  - bob -> likes -> charlie")
	fmt.Println("  - alice <-> friend <-> charlie")

	// 查询 alice 的所有关注关系
	follows, err := graph.GetNeighbors(ctx, "alice", "follows")
	if err != nil {
		log.Fatalf("查询失败: %v", err)
	}
	fmt.Printf("✅ alice 关注的人: %v\n", follows)

	// 查询 alice 的所有点赞关系
	likes, err := graph.GetNeighbors(ctx, "alice", "likes")
	if err != nil {
		log.Fatalf("查询失败: %v", err)
	}
	fmt.Printf("✅ alice 点赞的人: %v\n", likes)

	// 查询 alice 的所有朋友关系
	friends, err := graph.GetNeighbors(ctx, "alice", "friend")
	if err != nil {
		log.Fatalf("查询失败: %v", err)
	}
	fmt.Printf("✅ alice 的朋友: %v\n", friends)

	// ========== 示例 7: 删除关系 ==========
	fmt.Println("\n🗑️  示例 7: 删除关系...")

	// 删除 alice 对 bob 的关注关系
	if err := graph.Unlink(ctx, "alice", "follows", "bob"); err != nil {
		log.Fatalf("删除关系失败: %v", err)
	}
	fmt.Println("✅ 删除了 alice -> follows -> bob 关系")

	// 验证删除
	neighborsAfterDelete, err := graph.GetNeighbors(ctx, "alice", "follows")
	if err != nil {
		log.Fatalf("查询失败: %v", err)
	}
	fmt.Printf("✅ alice 现在关注的人: %v\n", neighborsAfterDelete)

	// ========== 示例 8: 复杂查询场景 ==========
	fmt.Println("\n🔍 示例 8: 复杂查询场景...")

	// 查找所有被多人关注的人（入度大于 1）
	fmt.Println("✅ 查找被多人关注的人:")
	allNodes := []string{"alice", "bob", "charlie", "david"}
	for _, node := range allNodes {
		inNeighbors, err := graph.GetInNeighbors(ctx, node, "follows")
		if err != nil {
			continue
		}
		if len(inNeighbors) > 1 {
			fmt.Printf("  - %s 被 %d 人关注: %v\n", node, len(inNeighbors), inNeighbors)
		}
	}

	// 查找所有关注多人的人（出度大于 1）
	fmt.Println("✅ 查找关注多人的人:")
	for _, node := range allNodes {
		outNeighbors, err := graph.GetNeighbors(ctx, node, "follows")
		if err != nil {
			continue
		}
		if len(outNeighbors) > 1 {
			fmt.Printf("  - %s 关注了 %d 人: %v\n", node, len(outNeighbors), outNeighbors)
		}
	}

	fmt.Println("\n🎉 所有示例完成！")
}
