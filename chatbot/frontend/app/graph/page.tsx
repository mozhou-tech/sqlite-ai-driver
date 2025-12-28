"use client";

import { useState, useEffect, useRef } from 'react';
import * as d3 from 'd3';
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { ArrowLeft, Loader2, ZoomIn, ZoomOut, Maximize2, Info, X } from "lucide-react";
import Link from 'next/link';
import { cn } from "@/lib/utils";

interface GraphNode extends d3.SimulationNodeDatum {
  id: string;
  name: string;
  type?: string;
  description?: string;
}

interface GraphLink extends d3.SimulationLinkDatum<GraphNode> {
  source: string | GraphNode;
  target: string | GraphNode;
  relation: string;
}

interface GraphData {
  entities: { name: string; type?: string; description?: string }[];
  relationships: { source: string; target: string; relation: string; description?: string }[];
}

const API_BASE_URL = process.env.NEXT_PUBLIC_API_URL || "http://localhost:8080/api";

export default function GraphPage() {
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const svgRef = useRef<SVGSVGElement>(null);
  const [graphData, setGraphData] = useState<GraphData | null>(null);
  const [selectedNode, setSelectedNode] = useState<GraphNode | null>(null);
  const [hoveredNode, setHoveredNode] = useState<string | null>(null);
  const [documents, setDocuments] = useState<any[]>([]);
  const [selectedDocId, setSelectedDocId] = useState<string>("");

  useEffect(() => {
    fetchDocuments();
  }, []);

  useEffect(() => {
    fetchGraph();
  }, [selectedDocId]);

  const fetchDocuments = async () => {
    try {
      const response = await fetch(`${API_BASE_URL}/documents`);
      if (response.ok) {
        const data = await response.json();
        setDocuments(data.documents || []);
      }
    } catch (error) {
      console.error("Failed to fetch documents:", error);
    }
  };

  const fetchGraph = async () => {
    setLoading(true);
    setError(null);
    try {
      const url = new URL(`${API_BASE_URL}/graph/full`);
      if (selectedDocId) {
        url.searchParams.append("doc_id", selectedDocId);
      }
      const response = await fetch(url.toString());
      if (!response.ok) {
        throw new Error('Failed to fetch graph data');
      }
      const data = await response.json();
      setGraphData(data);
    } catch (err: any) {
      setError(err.message || '获取图谱数据失败');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    if (!svgRef.current || !graphData || graphData.entities.length === 0) return;

    const svg = d3.select(svgRef.current);
    svg.selectAll('*').remove();

    const width = svgRef.current.clientWidth || 800;
    const height = svgRef.current.clientHeight || 600;

    const container = svg.append('g');

    // Zoom behavior
    const zoom = d3.zoom<SVGSVGElement, unknown>()
      .scaleExtent([0.1, 8])
      .on('zoom', (event) => {
        container.attr('transform', event.transform.toString());
      });

    svg.call(zoom);

    // Prepare data for D3
    const nodes: GraphNode[] = graphData.entities.map(e => ({ 
      id: e.name, 
      name: e.name,
      type: e.type,
      description: e.description
    }));
    const links: GraphLink[] = graphData.relationships.map(r => ({
      source: r.source,
      target: r.target,
      relation: r.relation
    }));

    // Simulation
    const simulation = d3.forceSimulation<GraphNode>(nodes)
      .force('link', d3.forceLink<GraphNode, GraphLink>(links).id(d => d.id).distance(180))
      .force('charge', d3.forceManyBody().strength(-400))
      .force('center', d3.forceCenter(width / 2, height / 2))
      .force('collision', d3.forceCollide().radius(60));

    // Arrow marker
    svg.append('defs').append('marker')
      .attr('id', 'arrowhead')
      .attr('viewBox', '-0 -5 10 10')
      .attr('refX', 28)
      .attr('refY', 0)
      .attr('orient', 'auto')
      .attr('markerWidth', 6)
      .attr('markerHeight', 6)
      .attr('xoverflow', 'visible')
      .append('svg:path')
      .attr('d', 'M 0,-5 L 10 ,0 L 0,5')
      .attr('fill', '#94a3b8')
      .style('stroke', 'none');

    // Draw links
    const link = container.append('g')
      .selectAll('g')
      .data(links)
      .enter()
      .append('g');

    const linkLine = link.append('line')
      .attr('stroke', '#cbd5e1')
      .attr('stroke-opacity', 0.6)
      .attr('stroke-width', 2)
      .attr('marker-end', 'url(#arrowhead)');

    // Link labels
    const linkLabels = link.append('text')
      .attr('font-size', '10px')
      .attr('fill', '#64748b')
      .attr('text-anchor', 'middle')
      .attr('dy', -5)
      .text(d => d.relation);

    // Draw nodes
    const node = container.append('g')
      .selectAll('g')
      .data(nodes)
      .enter()
      .append('g')
      .attr('class', 'cursor-pointer')
      .on('click', (event, d) => {
        event.stopPropagation();
        setSelectedNode(d);
      })
      .on('mouseover', (event, d) => {
        setHoveredNode(d.id);
        
        // Highlight connected nodes and links
        const neighbors = new Set<string>();
        neighbors.add(d.id);
        
        linkLine.style('stroke', (l: any) => {
          if (l.source.id === d.id || l.target.id === d.id) {
            neighbors.add(l.source.id);
            neighbors.add(l.target.id);
            return '#3b82f6';
          }
          return '#cbd5e1';
        }).style('stroke-opacity', (l: any) => (l.source.id === d.id || l.target.id === d.id) ? 1 : 0.2);
        
        nodeCircles.style('fill', (n: any) => neighbors.has(n.id) ? '#3b82f6' : '#94a3b8')
                   .style('opacity', (n: any) => neighbors.has(n.id) ? 1 : 0.3);
        
        nodeTexts.style('opacity', (n: any) => neighbors.has(n.id) ? 1 : 0.3)
                 .style('font-weight', (n: any) => neighbors.has(n.id) ? 'bold' : 'normal');
      })
      .on('mouseout', () => {
        setHoveredNode(null);
        linkLine.style('stroke', '#cbd5e1').style('stroke-opacity', 0.6);
        nodeCircles.style('fill', '#3b82f6').style('opacity', 1);
        nodeTexts.style('opacity', 1).style('font-weight', '500');
      })
      .call(d3.drag<SVGGElement, GraphNode>()
        .on('start', (event, d) => {
          if (!event.active) simulation.alphaTarget(0.3).restart();
          d.fx = d.x;
          d.fy = d.y;
        })
        .on('drag', (event, d) => {
          d.fx = event.x;
          d.fy = event.y;
        })
        .on('end', (event, d) => {
          if (!event.active) simulation.alphaTarget(0);
          d.fx = null;
          d.fy = null;
        }));

    const nodeCircles = node.append('circle')
      .attr('r', 12)
      .attr('fill', '#3b82f6')
      .attr('stroke', '#fff')
      .attr('stroke-width', 2)
      .attr('class', 'transition-colors duration-200');

    const nodeTexts = node.append('text')
      .attr('dy', 25)
      .attr('text-anchor', 'middle')
      .attr('font-size', '12px')
      .attr('font-weight', '500')
      .attr('fill', '#1e293b')
      .text(d => d.name);

    // Close detail panel when clicking on background
    svg.on('click', () => {
      setSelectedNode(null);
    });

    simulation.on('tick', () => {
      linkLine
        .attr('x1', d => (d.source as GraphNode).x!)
        .attr('y1', d => (d.source as GraphNode).y!)
        .attr('x2', d => (d.target as GraphNode).x!)
        .attr('y2', d => (d.target as GraphNode).y!);

      linkLabels
        .attr('x', d => ((d.source as GraphNode).x! + (d.target as GraphNode).x!) / 2)
        .attr('y', d => ((d.source as GraphNode).y! + (d.target as GraphNode).y!) / 2);

      node.attr('transform', d => `translate(${d.x},${d.y})`);
    });

    return () => {
      simulation.stop();
    };
  }, [graphData]);

  // Handle zoom controls
  const handleZoom = (scaleBy: number) => {
    const svg = d3.select(svgRef.current);
    const zoom = d3.zoom<SVGSVGElement, unknown>();
    // @ts-ignore
    svg.transition().duration(300).call(zoom.scaleBy, scaleBy);
  };

  const handleResetZoom = () => {
    const svg = d3.select(svgRef.current);
    const zoom = d3.zoom<SVGSVGElement, unknown>();
    // @ts-ignore
    svg.transition().duration(300).call(zoom.transform, d3.zoomIdentity);
  };

  return (
    <div className="flex flex-col h-screen bg-background">
      <header className="border-b p-4 flex justify-between items-center bg-card shadow-sm z-20">
        <div className="flex items-center gap-4">
          <Link href="/">
            <Button variant="ghost" size="icon" className="hover:bg-accent">
              <ArrowLeft className="h-5 w-5" />
            </Button>
          </Link>
          <div>
            <h1 className="text-2xl font-bold tracking-tight">知识库图谱</h1>
            <p className="text-sm text-muted-foreground">完整知识库实体关系可视化</p>
          </div>
        </div>
        <div className="flex items-center gap-4">
          <select
            value={selectedDocId}
            onChange={(e) => setSelectedDocId(e.target.value)}
            className="bg-background border rounded-md px-3 py-1.5 text-sm focus:outline-none focus:ring-2 focus:ring-primary max-w-[200px]"
            disabled={loading}
          >
            <option value="">全部文档图谱</option>
            {documents.map((doc) => (
              <option key={doc.id} value={doc.id}>
                {doc.content.substring(0, 30)}...
              </option>
            ))}
          </select>
          <div className="text-xs text-muted-foreground bg-muted px-2 py-1 rounded-full">
            {graphData?.entities.length || 0} 实体 | {graphData?.relationships.length || 0} 关系
          </div>
          <Button variant="outline" onClick={fetchGraph} disabled={loading} size="sm">
            {loading ? <Loader2 className="h-4 w-4 animate-spin mr-2" /> : null}
            刷新
          </Button>
        </div>
      </header>

      <main className="flex-1 overflow-hidden relative flex">
        {/* Detail Side Panel */}
        <div className={cn(
          "absolute left-0 top-0 bottom-0 w-80 bg-card border-r shadow-xl z-30 transition-transform duration-300 ease-in-out transform",
          selectedNode ? "translate-x-0" : "-translate-x-full"
        )}>
          {selectedNode && (
            <div className="h-full flex flex-col p-6">
              <div className="flex justify-between items-start mb-6">
                <h2 className="text-xl font-bold break-words pr-6">{selectedNode.name}</h2>
                <Button variant="ghost" size="icon" onClick={() => setSelectedNode(null)} className="h-8 w-8 -mt-1">
                  <X className="h-4 w-4" />
                </Button>
              </div>
              
              <div className="space-y-6 overflow-y-auto">
                <div>
                  <h3 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider mb-2">实体类型</h3>
                  <div className="inline-block px-2 py-1 bg-primary/10 text-primary text-xs font-medium rounded-md border border-primary/20">
                    {selectedNode.type || "未指定实体类型"}
                  </div>
                </div>

                <div>
                  <h3 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider mb-2">描述</h3>
                  <p className="text-sm leading-relaxed text-card-foreground/80 italic">
                    {selectedNode.description || "暂无此实体的详细描述。"}
                  </p>
                </div>

                <div>
                  <h3 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider mb-2">关联关系</h3>
                  <div className="space-y-2">
                    {graphData?.relationships
                      .filter(r => r.source === selectedNode.id || r.target === selectedNode.id)
                      .map((r, i) => (
                        <div key={i} className="text-xs p-3 bg-muted/50 rounded-lg border flex flex-col gap-1">
                          <div className="flex items-center gap-2 flex-wrap">
                            <span className={cn(
                              "font-bold",
                              r.source === selectedNode.id ? "text-primary" : "text-muted-foreground"
                            )}>{r.source}</span>
                            <span className="text-[10px] px-1.5 py-0.5 bg-accent rounded text-accent-foreground">{r.relation}</span>
                            <span className={cn(
                              "font-bold",
                              r.target === selectedNode.id ? "text-primary" : "text-muted-foreground"
                            )}>{r.target}</span>
                          </div>
                          {r.description && <p className="text-[10px] text-muted-foreground mt-1">{r.description}</p>}
                        </div>
                      ))}
                  </div>
                </div>
              </div>
            </div>
          )}
        </div>

        <div className="flex-1 relative bg-slate-50">
          {loading && !graphData && (
            <div className="absolute inset-0 flex items-center justify-center bg-background/50 z-10">
              <div className="flex flex-col items-center gap-4">
                <Loader2 className="h-10 w-10 animate-spin text-primary" />
                <p className="text-sm font-medium">加载图谱数据中...</p>
              </div>
            </div>
          )}
          
          {error && (
            <div className="absolute inset-0 flex items-center justify-center z-10 p-4">
              <Card className="w-full max-w-md border-destructive shadow-2xl">
                <CardHeader>
                  <CardTitle className="text-destructive flex items-center gap-2">
                    <X className="h-5 w-5" />
                    发生错误
                  </CardTitle>
                </CardHeader>
                <CardContent>
                  <p className="text-muted-foreground">{error}</p>
                  <Button className="mt-6 w-full" onClick={fetchGraph}>重试获取数据</Button>
                </CardContent>
              </Card>
            </div>
          )}

          {!loading && graphData && graphData.entities.length === 0 && (
            <div className="absolute inset-0 flex items-center justify-center z-10 p-4">
              <Card className="w-full max-w-md shadow-2xl">
                <CardHeader>
                  <CardTitle className="flex items-center gap-2">
                    <Info className="h-5 w-5 text-primary" />
                    暂无数据
                  </CardTitle>
                </CardHeader>
                <CardContent>
                  <p className="text-muted-foreground leading-relaxed">
                    知识库中还没有提取出实体和关系。LLM 会在您上传文档后自动分析并构建图谱。
                  </p>
                  <Link href="/" className="block mt-6">
                    <Button className="w-full shadow-lg">返回主页上传文档</Button>
                  </Link>
                </CardContent>
              </Card>
            </div>
          )}

          <div className="w-full h-full overflow-hidden">
            <svg ref={svgRef} className="w-full h-full cursor-grab active:cursor-grabbing" />
          </div>

          <div className="absolute bottom-8 right-8 flex flex-col gap-4">
            <Card className="p-1 flex flex-col gap-1 shadow-2xl bg-background/90 backdrop-blur-sm border-border/50">
               <Button variant="ghost" size="icon" title="放大" onClick={() => handleZoom(1.5)} className="h-10 w-10">
                 <ZoomIn className="h-5 w-5" />
               </Button>
               <Button variant="ghost" size="icon" title="缩小" onClick={() => handleZoom(0.75)} className="h-10 w-10">
                 <ZoomOut className="h-5 w-5" />
               </Button>
               <Button variant="ghost" size="icon" title="重置视图" onClick={handleResetZoom} className="h-10 w-10 border-t rounded-none">
                 <Maximize2 className="h-5 w-5" />
               </Button>
            </Card>

            <div className="bg-background/90 backdrop-blur-sm p-4 rounded-xl border shadow-2xl max-w-xs animate-in fade-in slide-in-from-bottom-4 duration-700">
              <h4 className="text-xs font-bold uppercase tracking-widest text-muted-foreground mb-3 flex items-center gap-2">
                <Info className="h-3 w-3" />
                图谱交互指南
              </h4>
              <ul className="text-[10px] space-y-2 text-muted-foreground">
                <li className="flex items-center gap-2">
                  <span className="w-1.5 h-1.5 rounded-full bg-primary" />
                  <strong>拖拽</strong> 节点可固定位置
                </li>
                <li className="flex items-center gap-2">
                  <span className="w-1.5 h-1.5 rounded-full bg-primary" />
                  <strong>滚动</strong> 鼠标滑轮进行缩放
                </li>
                <li className="flex items-center gap-2">
                  <span className="w-1.5 h-1.5 rounded-full bg-primary" />
                  <strong>点击</strong> 节点查看详细信息
                </li>
                <li className="flex items-center gap-2">
                  <span className="w-1.5 h-1.5 rounded-full bg-primary" />
                  <strong>悬停</strong> 节点可高亮关联路径
                </li>
              </ul>
            </div>
          </div>
        </div>
      </main>
    </div>
  );
}
