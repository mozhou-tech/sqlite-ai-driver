import { useState, useEffect, useRef, useCallback } from 'react'
import * as d3 from 'd3'
import { apiClient, GraphQueryResult } from '../utils/api'
import { Button } from '../components/ui/Button'
import { Card, CardContent, CardHeader, CardTitle } from '../components/ui/Card'
import { Input } from '../components/ui/Input'

interface GraphNode {
  id: string
  label: string
  x?: number
  y?: number
  fx?: number | null
  fy?: number | null
}

interface GraphLink {
  source: string | GraphNode
  target: string | GraphNode
  relation: string
}

interface D3GraphLink extends GraphLink {
  source: GraphNode
  target: GraphNode
}

interface GraphData {
  nodes: GraphNode[]
  links: GraphLink[]
}

export default function GraphPage() {
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  
  // 创建链接
  const [linkFrom, setLinkFrom] = useState('')
  const [linkRelation, setLinkRelation] = useState('follows')
  const [linkTo, setLinkTo] = useState('')
  
  // 查询邻居
  const [neighborNodeId, setNeighborNodeId] = useState('user1')
  const [neighborRelation, setNeighborRelation] = useState('follows')
  const [neighbors, setNeighbors] = useState<string[]>([])
  
  // 查找路径
  const [pathFrom, setPathFrom] = useState('user1')
  const [pathTo, setPathTo] = useState('user4')
  const [pathMaxDepth, setPathMaxDepth] = useState(5)
  const [paths, setPaths] = useState<string[][]>([])
  
  // 图查询
  const [queryString, setQueryString] = useState("V('user1').Out('follows')")
  const [queryResults, setQueryResults] = useState<GraphQueryResult[]>([])
  
  // 图谱可视化
  const svgRef = useRef<SVGSVGElement>(null)
  const [graphData, setGraphData] = useState<GraphData>({ nodes: [], links: [] })

  const handleCreateLink = async () => {
    if (!linkFrom || !linkRelation || !linkTo) {
      setError('请填写所有字段')
      return
    }

    setLoading(true)
    setError(null)
    try {
      await apiClient.graphLink(linkFrom, linkRelation, linkTo)
      alert('链接创建成功！')
      setLinkFrom('')
      setLinkTo('')
      // 刷新邻居列表
      if (neighborNodeId === linkFrom) {
        loadNeighbors()
      }
    } catch (err: unknown) {
      const error = err as { message?: string }
      setError(error.message || '创建链接失败')
    } finally {
      setLoading(false)
    }
  }

  const handleDeleteLink = async () => {
    if (!linkFrom || !linkRelation || !linkTo) {
      setError('请填写所有字段')
      return
    }

    if (!confirm('确定要删除这个链接吗？')) return

    setLoading(true)
    setError(null)
    try {
      await apiClient.graphUnlink(linkFrom, linkRelation, linkTo)
      alert('链接删除成功！')
      setLinkFrom('')
      setLinkTo('')
      // 刷新邻居列表
      if (neighborNodeId === linkFrom) {
        loadNeighbors()
      }
    } catch (err: unknown) {
      const error = err as { message?: string }
      setError(error.message || '删除链接失败')
    } finally {
      setLoading(false)
    }
  }

  const loadNeighbors = async () => {
    if (!neighborNodeId) return

    setLoading(true)
    setError(null)
    try {
      const response = await apiClient.graphNeighbors(
        neighborNodeId,
        neighborRelation || undefined
      )
      setNeighbors(response.neighbors || [])
    } catch (err: unknown) {
      const error = err as { message?: string }
      setError(error.message || '查询邻居失败')
    } finally {
      setLoading(false)
    }
  }

  const handleFindPath = async () => {
    if (!pathFrom || !pathTo) {
      setError('请填写起始节点和目标节点')
      return
    }

    setLoading(true)
    setError(null)
    try {
      const response = await apiClient.graphPath(pathFrom, pathTo, pathMaxDepth)
      setPaths(response.paths || [])
    } catch (err: unknown) {
      const error = err as { message?: string }
      setError(error.message || '查找路径失败')
    } finally {
      setLoading(false)
    }
  }

  const handleQuery = async () => {
    if (!queryString) {
      setError('请输入查询字符串')
      return
    }

    setLoading(true)
    setError(null)
    try {
      const response = await apiClient.graphQuery(queryString)
      setQueryResults(response.results || [])
      // 更新图谱数据
      updateGraphFromQueryResults(response.results || [])
    } catch (err: unknown) {
      const error = err as { message?: string }
      setError(error.message || '执行查询失败')
    } finally {
      setLoading(false)
    }
  }

  // 从查询结果更新图谱数据
  const updateGraphFromQueryResults = useCallback((results: GraphQueryResult[]) => {
    const nodeMap = new Map<string, GraphNode>()
    const links: GraphLink[] = []

    results.forEach((result) => {
      // 添加节点
      if (!nodeMap.has(result.subject)) {
        nodeMap.set(result.subject, { id: result.subject, label: result.subject })
      }
      if (!nodeMap.has(result.object)) {
        nodeMap.set(result.object, { id: result.object, label: result.object })
      }
      // 添加边
      links.push({
        source: result.subject,
        target: result.object,
        relation: result.predicate,
      })
    })

    setGraphData({
      nodes: Array.from(nodeMap.values()),
      links: links,
    })
  }, [])

  // 从邻居数据更新图谱
  const updateGraphFromNeighbors = useCallback((nodeId: string, neighbors: string[], relation: string) => {
    const nodes: GraphNode[] = [{ id: nodeId, label: nodeId }]
    const links: GraphLink[] = []

    neighbors.forEach((neighbor) => {
      nodes.push({ id: neighbor, label: neighbor })
      links.push({
        source: nodeId,
        target: neighbor,
        relation: relation,
      })
    })

    setGraphData({ nodes, links })
  }, [])

  // 从路径数据更新图谱
  const updateGraphFromPaths = useCallback((paths: string[][]) => {
    const nodeMap = new Map<string, GraphNode>()
    const links: GraphLink[] = []

    paths.forEach((path) => {
      for (let i = 0; i < path.length; i++) {
        if (!nodeMap.has(path[i])) {
          nodeMap.set(path[i], { id: path[i], label: path[i] })
        }
        if (i < path.length - 1) {
          links.push({
            source: path[i],
            target: path[i + 1],
            relation: 'path',
          })
        }
      }
    })

    setGraphData({
      nodes: Array.from(nodeMap.values()),
      links: links,
    })
  }, [])

  // 渲染 d3 图谱
  useEffect(() => {
    if (!svgRef.current || graphData.nodes.length === 0) return

    const svg = d3.select(svgRef.current)
    svg.selectAll('*').remove()

    const width = svgRef.current.clientWidth || 800
    const height = 600

    svg.attr('width', width).attr('height', height)

    // 创建缩放和平移
    const zoom = d3.zoom<SVGSVGElement, unknown>()
      .scaleExtent([0.1, 4])
      .on('zoom', (event: d3.D3ZoomEvent<SVGSVGElement, unknown>) => {
        container.attr('transform', event.transform.toString())
      })

    svg.call(zoom)

    const container = svg.append('g')

    // 创建力导向图
    const simulation = d3.forceSimulation<GraphNode>(graphData.nodes)
      .force('link', d3.forceLink<GraphNode, GraphLink>(graphData.links).id((d: GraphNode) => d.id).distance(100))
      .force('charge', d3.forceManyBody<GraphNode>().strength(-300))
      .force('center', d3.forceCenter(width / 2, height / 2))
      .force('collision', d3.forceCollide<GraphNode>().radius(30))

    // 绘制边
    const link = container.append('g')
      .selectAll('line')
      .data(graphData.links)
      .enter()
      .append('line')
      .attr('stroke', '#999')
      .attr('stroke-opacity', 0.6)
      .attr('stroke-width', 2)

    // 绘制边的标签
    const linkLabels = container.append('g')
      .selectAll('text')
      .data(graphData.links)
      .enter()
      .append('text')
      .attr('font-size', '10px')
      .attr('fill', '#666')
      .text((d: GraphLink) => d.relation)

    // 绘制节点
    const node = container.append('g')
      .selectAll('circle')
      .data(graphData.nodes)
      .enter()
      .append('circle')
      .attr('r', 20)
      .attr('fill', '#3b82f6')
      .attr('stroke', '#fff')
      .attr('stroke-width', 2)
      .call(d3.drag<SVGCircleElement, GraphNode>()
        .on('start', dragstarted)
        .on('drag', dragged)
        .on('end', dragended))

    // 绘制节点标签
    const nodeLabels = container.append('g')
      .selectAll('text')
      .data(graphData.nodes)
      .enter()
      .append('text')
      .attr('font-size', '12px')
      .attr('fill', '#000')
      .attr('text-anchor', 'middle')
      .attr('dy', 35)
      .text((d: GraphNode) => d.label)

    // 拖拽函数
    function dragstarted(event: d3.D3DragEvent<SVGCircleElement, GraphNode, GraphNode>, d: GraphNode) {
      if (!event.active) simulation.alphaTarget(0.3).restart()
      d.fx = d.x
      d.fy = d.y
    }

    function dragged(event: d3.D3DragEvent<SVGCircleElement, GraphNode, GraphNode>, d: GraphNode) {
      d.fx = event.x
      d.fy = event.y
    }

    function dragended(event: d3.D3DragEvent<SVGCircleElement, GraphNode, GraphNode>, d: GraphNode) {
      if (!event.active) simulation.alphaTarget(0)
      d.fx = null
      d.fy = null
    }

    // 更新位置
    simulation.on('tick', () => {
      link
        .attr('x1', (d) => {
          const link = d as unknown as D3GraphLink
          return link.source.x ?? 0
        })
        .attr('y1', (d) => {
          const link = d as unknown as D3GraphLink
          return link.source.y ?? 0
        })
        .attr('x2', (d) => {
          const link = d as unknown as D3GraphLink
          return link.target.x ?? 0
        })
        .attr('y2', (d) => {
          const link = d as unknown as D3GraphLink
          return link.target.y ?? 0
        })

      linkLabels
        .attr('x', (d) => {
          const link = d as unknown as D3GraphLink
          return ((link.source.x ?? 0) + (link.target.x ?? 0)) / 2
        })
        .attr('y', (d) => {
          const link = d as unknown as D3GraphLink
          return ((link.source.y ?? 0) + (link.target.y ?? 0)) / 2
        })

      node
        .attr('cx', (d: GraphNode) => d.x ?? 0)
        .attr('cy', (d: GraphNode) => d.y ?? 0)

      nodeLabels
        .attr('x', (d: GraphNode) => d.x ?? 0)
        .attr('y', (d: GraphNode) => d.y ?? 0)
    })

    // 清理函数
    return () => {
      simulation.stop()
    }
  }, [graphData])

  // 当邻居数据变化时更新图谱
  useEffect(() => {
    if (neighbors.length > 0) {
      updateGraphFromNeighbors(neighborNodeId, neighbors, neighborRelation)
    }
  }, [neighbors, neighborNodeId, neighborRelation, updateGraphFromNeighbors])

  // 当路径数据变化时更新图谱
  useEffect(() => {
    if (paths.length > 0) {
      updateGraphFromPaths(paths)
    }
  }, [paths, updateGraphFromPaths])

  useEffect(() => {
    loadNeighbors()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [neighborNodeId, neighborRelation])

  return (
    <div className="space-y-6">
      <Card>
        <CardHeader>
          <CardTitle>图数据库演示</CardTitle>
        </CardHeader>
        <CardContent>
          <p className="text-sm text-muted-foreground mb-4">
            演示图数据库的基本操作：创建链接、查询邻居、查找路径和查询图数据。
          </p>

          {error && (
            <div className="mb-4 p-4 bg-destructive/10 text-destructive rounded-md">
              {error}
            </div>
          )}

          {/* 创建/删除链接 */}
          <div className="mb-6 p-4 border rounded-md">
            <h3 className="text-lg font-semibold mb-4">创建/删除链接</h3>
            <div className="grid grid-cols-1 md:grid-cols-4 gap-4">
              <div>
                <label className="block text-sm font-medium mb-1">起始节点</label>
                <Input
                  value={linkFrom}
                  onChange={(e) => setLinkFrom(e.target.value)}
                  placeholder="例如: user1"
                />
              </div>
              <div>
                <label className="block text-sm font-medium mb-1">关系</label>
                <Input
                  value={linkRelation}
                  onChange={(e) => setLinkRelation(e.target.value)}
                  placeholder="例如: follows"
                />
              </div>
              <div>
                <label className="block text-sm font-medium mb-1">目标节点</label>
                <Input
                  value={linkTo}
                  onChange={(e) => setLinkTo(e.target.value)}
                  placeholder="例如: user2"
                />
              </div>
              <div className="flex items-end gap-2">
                <Button onClick={handleCreateLink} disabled={loading}>
                  创建链接
                </Button>
                <Button
                  variant="destructive"
                  onClick={handleDeleteLink}
                  disabled={loading}
                >
                  删除链接
                </Button>
              </div>
            </div>
          </div>

          {/* 查询邻居 */}
          <div className="mb-6 p-4 border rounded-md">
            <h3 className="text-lg font-semibold mb-4">查询邻居节点</h3>
            <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
              <div>
                <label className="block text-sm font-medium mb-1">节点ID</label>
                <Input
                  value={neighborNodeId}
                  onChange={(e) => setNeighborNodeId(e.target.value)}
                  placeholder="例如: user1"
                />
              </div>
              <div>
                <label className="block text-sm font-medium mb-1">关系（可选）</label>
                <Input
                  value={neighborRelation}
                  onChange={(e) => setNeighborRelation(e.target.value)}
                  placeholder="例如: follows"
                />
              </div>
              <div className="flex items-end gap-2">
                <Button onClick={loadNeighbors} disabled={loading}>
                  查询邻居
                </Button>
                <Button
                  variant="outline"
                  onClick={() => updateGraphFromNeighbors(neighborNodeId, neighbors, neighborRelation)}
                  disabled={loading || neighbors.length === 0}
                >
                  可视化邻居
                </Button>
              </div>
            </div>
            {neighbors.length > 0 && (
              <div className="mt-4">
                <p className="text-sm font-medium mb-2">邻居节点:</p>
                <div className="flex flex-wrap gap-2">
                  {neighbors.map((node) => (
                    <span
                      key={node}
                      className="px-3 py-1 bg-primary/10 text-primary rounded-md"
                    >
                      {node}
                    </span>
                  ))}
                </div>
              </div>
            )}
          </div>

          {/* 查找路径 */}
          <div className="mb-6 p-4 border rounded-md">
            <h3 className="text-lg font-semibold mb-4">查找路径</h3>
            <div className="grid grid-cols-1 md:grid-cols-4 gap-4">
              <div>
                <label className="block text-sm font-medium mb-1">起始节点</label>
                <Input
                  value={pathFrom}
                  onChange={(e) => setPathFrom(e.target.value)}
                  placeholder="例如: user1"
                />
              </div>
              <div>
                <label className="block text-sm font-medium mb-1">目标节点</label>
                <Input
                  value={pathTo}
                  onChange={(e) => setPathTo(e.target.value)}
                  placeholder="例如: user4"
                />
              </div>
              <div>
                <label className="block text-sm font-medium mb-1">最大深度</label>
                <Input
                  type="number"
                  value={pathMaxDepth}
                  onChange={(e) => setPathMaxDepth(parseInt(e.target.value) || 5)}
                  min="1"
                  max="10"
                />
              </div>
              <div className="flex items-end gap-2">
                <Button onClick={handleFindPath} disabled={loading}>
                  查找路径
                </Button>
                <Button
                  variant="outline"
                  onClick={() => updateGraphFromPaths(paths)}
                  disabled={loading || paths.length === 0}
                >
                  可视化路径
                </Button>
              </div>
            </div>
            {paths.length > 0 && (
              <div className="mt-4">
                <p className="text-sm font-medium mb-2">找到 {paths.length} 条路径:</p>
                <div className="space-y-2">
                  {paths.map((path, index) => (
                    <div
                      key={index}
                      className="p-3 bg-muted rounded-md text-sm"
                    >
                      {path.join(' → ')}
                    </div>
                  ))}
                </div>
              </div>
            )}
          </div>

          {/* 图查询 */}
          <div className="p-4 border rounded-md">
            <h3 className="text-lg font-semibold mb-4">图查询</h3>
            <div className="space-y-4">
              <div>
                <label className="block text-sm font-medium mb-1">查询字符串</label>
                <Input
                  value={queryString}
                  onChange={(e) => setQueryString(e.target.value)}
                  placeholder="例如: V('user1').Out('follows')"
                />
                <p className="text-xs text-muted-foreground mt-1">
                  支持格式: V('nodeId').Out('relation') 或 V('nodeId').In('relation')
                </p>
              </div>
              <div className="flex gap-2">
                <Button onClick={handleQuery} disabled={loading}>
                  执行查询
                </Button>
                <Button
                  variant="outline"
                  onClick={() => updateGraphFromQueryResults(queryResults)}
                  disabled={loading || queryResults.length === 0}
                >
                  可视化查询结果
                </Button>
              </div>
              
              {/* 查询结果和可视化左右分栏 */}
              {(queryResults.length > 0 || graphData.nodes.length > 0) && (
                <div className="mt-4 grid grid-cols-1 lg:grid-cols-2 gap-4">
                  {/* 左侧：查询结果列表 */}
                  {queryResults.length > 0 && (
                    <div>
                      <p className="text-sm font-medium mb-2">查询结果 ({queryResults.length} 条):</p>
                      <div className="space-y-2 max-h-[600px] overflow-y-auto">
                        {queryResults.map((result, index) => (
                          <div
                            key={index}
                            className="p-3 bg-muted rounded-md text-sm"
                          >
                            <span className="font-semibold">{result.subject}</span>
                            {' --'}
                            <span className="font-semibold text-primary">{result.predicate}</span>
                            {'--> '}
                            <span className="font-semibold">{result.object}</span>
                          </div>
                        ))}
                      </div>
                    </div>
                  )}
                  
                  {/* 右侧：图谱可视化 */}
                  {graphData.nodes.length > 0 && (
                    <div>
                      <h3 className="text-lg font-semibold mb-4">图谱可视化</h3>
                      <div className="w-full border rounded-md overflow-hidden bg-white">
                        <svg
                          ref={svgRef}
                          className="w-full"
                          style={{ height: '600px', minHeight: '600px' }}
                        />
                      </div>
                      <p className="text-xs text-muted-foreground mt-2">
                        💡 提示: 可以拖拽节点移动位置，使用鼠标滚轮缩放图谱
                      </p>
                    </div>
                  )}
                </div>
              )}
            </div>
          </div>

          {/* 示例数据说明 */}
          <div className="mt-6 p-4 bg-muted rounded-md">
            <h4 className="font-semibold mb-2">示例数据说明</h4>
            <p className="text-sm text-muted-foreground mb-2">
              运行 <code className="px-1 py-0.5 bg-background rounded">make seed</code> 或 <code className="px-1 py-0.5 bg-background rounded">cd api && go run seed.go</code> 会生成以下示例数据：
            </p>
            <ul className="text-sm text-muted-foreground mt-2 list-disc list-inside space-y-1">
              <li><strong>用户集合 (users):</strong> user1 (Alice), user2 (Bob), user3 (Charlie), user4 (Diana), user5 (Eve)</li>
              <li><strong>图关系 (follows):</strong> 
                <ul className="ml-4 mt-1 space-y-0.5">
                  <li>user1 → user2, user1 → user3</li>
                  <li>user2 → user3, user2 → user4</li>
                  <li>user3 → user4</li>
                  <li>user4 → user1</li>
                  <li>user5 → user1, user5 → user2</li>
                </ul>
              </li>
              <li className="mt-2"><strong>测试建议:</strong></li>
              <li className="ml-4">查询 user1 的邻居: 节点ID = user1, 关系 = follows</li>
              <li className="ml-4">查找路径: 从 user1 到 user4 (应该找到路径: user1 → user2 → user4 或 user1 → user3 → user4)</li>
              <li className="ml-4">图查询: 使用 <code className="px-1 py-0.5 bg-background rounded">V('user1').Out('follows')</code> 查询 user1 关注的所有人</li>
            </ul>
            <p className="text-xs text-muted-foreground mt-3 p-2 bg-background rounded">
              💡 提示: 如果图查询没有返回结果，请检查：
              <br />1. 是否运行了 <code className="px-1 py-0.5 bg-muted rounded">make seed</code> 生成数据
              <br />2. API 服务器日志中是否有图关系创建成功的消息
              <br />3. 图数据库是否正确初始化（检查 API 启动日志）
            </p>
          </div>
        </CardContent>
      </Card>
    </div>
  )
}

