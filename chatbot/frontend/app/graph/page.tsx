"use client";

import { useState, useEffect, useRef } from 'react';
import * as d3 from 'd3';
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { ArrowLeft, Loader2, ZoomIn, ZoomOut, Maximize2 } from "lucide-react";
import Link from 'next/link';

interface GraphNode extends d3.SimulationNodeDatum {
  id: string;
  name: string;
}

interface GraphLink extends d3.SimulationLinkDatum<GraphNode> {
  source: string | GraphNode;
  target: string | GraphNode;
  relation: string;
}

interface GraphData {
  entities: { name: string }[];
  relationships: { source: string; target: string; relation: string }[];
}

const API_BASE_URL = process.env.NEXT_PUBLIC_API_URL || "http://localhost:8080/api";

export default function GraphPage() {
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const svgRef = useRef<SVGSVGElement>(null);
  const [graphData, setGraphData] = useState<GraphData | null>(null);

  useEffect(() => {
    fetchGraph();
  }, []);

  const fetchGraph = async () => {
    setLoading(true);
    setError(null);
    try {
      const response = await fetch(`${API_BASE_URL}/graph/full`);
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
    const nodes: GraphNode[] = graphData.entities.map(e => ({ id: e.name, name: e.name }));
    const links: GraphLink[] = graphData.relationships.map(r => ({
      source: r.source,
      target: r.target,
      relation: r.relation
    }));

    // Simulation
    const simulation = d3.forceSimulation<GraphNode>(nodes)
      .force('link', d3.forceLink<GraphNode, GraphLink>(links).id(d => d.id).distance(150))
      .force('charge', d3.forceManyBody().strength(-300))
      .force('center', d3.forceCenter(width / 2, height / 2))
      .force('collision', d3.forceCollide().radius(50));

    // Arrow marker
    svg.append('defs').append('marker')
      .attr('id', 'arrowhead')
      .attr('viewBox', '-0 -5 10 10')
      .attr('refX', 25)
      .attr('refY', 0)
      .attr('orient', 'auto')
      .attr('markerWidth', 6)
      .attr('markerHeight', 6)
      .attr('xoverflow', 'visible')
      .append('svg:path')
      .attr('d', 'M 0,-5 L 10 ,0 L 0,5')
      .attr('fill', '#999')
      .style('stroke', 'none');

    // Draw links
    const link = container.append('g')
      .selectAll('line')
      .data(links)
      .enter()
      .append('line')
      .attr('stroke', '#999')
      .attr('stroke-opacity', 0.6)
      .attr('stroke-width', 1.5)
      .attr('marker-end', 'url(#arrowhead)');

    // Link labels
    const linkLabels = container.append('g')
      .selectAll('text')
      .data(links)
      .enter()
      .append('text')
      .attr('font-size', '8px')
      .attr('fill', '#999')
      .attr('text-anchor', 'middle')
      .text(d => d.relation);

    // Draw nodes
    const node = container.append('g')
      .selectAll('g')
      .data(nodes)
      .enter()
      .append('g')
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

    node.append('circle')
      .attr('r', 10)
      .attr('fill', '#3b82f6')
      .attr('stroke', '#fff')
      .attr('stroke-width', 2);

    node.append('text')
      .attr('dy', 20)
      .attr('text-anchor', 'middle')
      .attr('font-size', '10px')
      .attr('font-weight', '500')
      .text(d => d.name);

    simulation.on('tick', () => {
      link
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

  return (
    <div className="flex flex-col h-screen bg-background">
      <header className="border-b p-4 flex justify-between items-center">
        <div className="flex items-center gap-4">
          <Link href="/">
            <Button variant="ghost" size="icon">
              <ArrowLeft className="h-5 w-5" />
            </Button>
          </Link>
          <div>
            <h1 className="text-2xl font-bold">知识库图谱</h1>
            <p className="text-sm text-muted-foreground">完整知识库实体关系可视化</p>
          </div>
        </div>
        <div className="flex gap-2">
          <Button variant="outline" onClick={fetchGraph} disabled={loading}>
            {loading ? <Loader2 className="h-4 w-4 animate-spin mr-2" /> : null}
            刷新数据
          </Button>
        </div>
      </header>

      <main className="flex-1 overflow-hidden p-4 relative">
        {loading && !graphData && (
          <div className="absolute inset-0 flex items-center justify-center bg-background/50 z-10">
            <Loader2 className="h-8 w-8 animate-spin text-primary" />
          </div>
        )}
        
        {error && (
          <div className="absolute inset-0 flex items-center justify-center z-10">
            <Card className="w-96 border-destructive">
              <CardHeader>
                <CardTitle className="text-destructive">发生错误</CardTitle>
              </CardHeader>
              <CardContent>
                <p>{error}</p>
                <Button className="mt-4 w-full" onClick={fetchGraph}>重试</Button>
              </CardContent>
            </Card>
          </div>
        )}

        {!loading && graphData && graphData.entities.length === 0 && (
          <div className="absolute inset-0 flex items-center justify-center z-10">
            <Card className="w-96">
              <CardHeader>
                <CardTitle>暂无数据</CardTitle>
              </CardHeader>
              <CardContent>
                <p className="text-muted-foreground">知识库中还没有提取出实体和关系，请先上传文档。</p>
                <Link href="/" className="block mt-4">
                  <Button className="w-full">返回主页上传文档</Button>
                </Link>
              </CardContent>
            </Card>
          </div>
        )}

        <div className="w-full h-full border rounded-lg bg-card overflow-hidden shadow-inner">
          <svg ref={svgRef} className="w-full h-full cursor-move" />
        </div>

        <div className="absolute bottom-8 right-8 flex flex-col gap-2">
          <Card className="p-2 flex flex-col gap-2 shadow-lg bg-background/80 backdrop-blur-sm">
             <Button variant="ghost" size="icon" title="放大" onClick={() => {
                const svg = d3.select(svgRef.current);
                // @ts-ignore
                svg.transition().call(d3.zoom().scaleBy, 1.5);
             }}>
               <ZoomIn className="h-4 w-4" />
             </Button>
             <Button variant="ghost" size="icon" title="缩小" onClick={() => {
                const svg = d3.select(svgRef.current);
                // @ts-ignore
                svg.transition().call(d3.zoom().scaleBy, 0.75);
             }}>
               <ZoomOut className="h-4 w-4" />
             </Button>
             <Button variant="ghost" size="icon" title="重置" onClick={() => {
                const svg = d3.select(svgRef.current);
                // @ts-ignore
                svg.transition().call(d3.zoom().transform, d3.zoomIdentity);
             }}>
               <Maximize2 className="h-4 w-4" />
             </Button>
          </Card>
        </div>
      </main>
    </div>
  );
}

