import { useState } from 'react'
import { apiClient, VectorSearchResult, VectorSearchResponse } from '../utils/api'
import { Button } from '../components/ui/Button'
import { Input } from '../components/ui/Input'
import { Card, CardContent, CardHeader, CardTitle } from '../components/ui/Card'
import { JsonViewer } from '../components/JsonViewer'

export default function VectorSearchPage() {
  const [collection, setCollection] = useState('products')
  const [queryType, setQueryType] = useState<'text' | 'vector'>('text')
  const [queryText, setQueryText] = useState('')
  const [queryVector, setQueryVector] = useState('')
  const [field, setField] = useState('embedding')
  const [results, setResults] = useState<VectorSearchResult[]>([])
  const [took, setTook] = useState<number | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [limit, setLimit] = useState(10)

  const handleSearch = async () => {
    if (!collection.trim()) {
      setError('请输入集合名称')
      return
    }

    setLoading(true)
    setError(null)
    setTook(null)
    try {
      let response: VectorSearchResponse
      
      if (queryType === 'text') {
        // 文本查询：使用 DashScope 生成 embedding
        if (!queryText.trim()) {
          setError('请输入查询文本')
          setLoading(false)
          return
        }
        response = await apiClient.vectorSearch(
          collection,
          undefined,
          limit,
          field,
          queryText
        )
      } else {
        // 向量查询：解析向量字符串
        if (!queryVector.trim()) {
          setError('请输入查询向量')
          setLoading(false)
          return
        }
        
        let vector: number[]
        try {
          if (queryVector.trim().startsWith('[')) {
            vector = JSON.parse(queryVector)
          } else {
            vector = queryVector
              .split(',')
              .map((s) => parseFloat(s.trim()))
              .filter((n) => !isNaN(n))
          }
        } catch (err) {
          setError('无效的向量格式。请输入 JSON 数组或逗号分隔的数字')
          setLoading(false)
          return
        }

        if (vector.length === 0) {
          setError('向量不能为空')
          setLoading(false)
          return
        }

        response = await apiClient.vectorSearch(
          collection,
          vector,
          limit,
          field
        )
      }
      
      setResults(response.results)
      setTook(response.took)
    } catch (err: unknown) {
      const error = err as { message?: string }
      setError(error.message || '搜索失败')
      setResults([])
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="space-y-6">
      <Card>
        <CardHeader>
          <CardTitle>向量搜索</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="space-y-4">
            <div className="flex gap-4">
              <Input
                placeholder="集合名称 (例如: products)"
                value={collection}
                onChange={(e) => setCollection(e.target.value)}
                className="flex-1"
              />
              <Input
                placeholder="向量字段名 (默认: embedding)"
                value={field}
                onChange={(e) => setField(e.target.value)}
                className="w-48"
              />
            </div>

            {/* 查询类型选择 */}
            <div>
              <label className="block text-sm font-medium mb-2">查询类型:</label>
              <div className="flex gap-2">
                <Button
                  variant={queryType === 'text' ? 'default' : 'outline'}
                  size="sm"
                  onClick={() => setQueryType('text')}
                >
                  文本查询（自动生成 embedding）
                </Button>
                <Button
                  variant={queryType === 'vector' ? 'default' : 'outline'}
                  size="sm"
                  onClick={() => setQueryType('vector')}
                >
                  向量查询
                </Button>
              </div>
            </div>

            {/* 文本查询输入 */}
            {queryType === 'text' && (
              <div>
                <label className="block text-sm font-medium mb-2">
                  查询文本（将使用 DashScope 自动生成 embedding）
                </label>
                <Input
                  placeholder="例如: 智能手机、运动鞋、笔记本电脑等"
                  value={queryText}
                  onChange={(e) => setQueryText(e.target.value)}
                  onKeyPress={(e) => e.key === 'Enter' && handleSearch()}
                />
              </div>
            )}

            {/* 向量查询输入 */}
            {queryType === 'vector' && (
              <div>
                <label className="block text-sm font-medium mb-2">
                  查询向量 (JSON 数组或逗号分隔的数字，例如: [0.1, 0.2, 0.3] 或 0.1, 0.2, 0.3)
                </label>
                <Input
                  placeholder="[0.1, 0.2, 0.3, ...] 或 0.1, 0.2, 0.3, ..."
                  value={queryVector}
                  onChange={(e) => setQueryVector(e.target.value)}
                  onKeyPress={(e) => e.key === 'Enter' && handleSearch()}
                  className="font-mono"
                />
              </div>
            )}

            <div className="flex gap-4">
              <Input
                type="number"
                placeholder="结果数量"
                value={limit}
                onChange={(e) => setLimit(parseInt(e.target.value) || 10)}
                className="w-32"
              />
              <Button onClick={handleSearch} disabled={loading}>
                {loading ? '搜索中...' : '搜索'}
              </Button>
            </div>

            {error && (
              <div className="p-4 bg-destructive/10 text-destructive rounded-md">
                {error}
              </div>
            )}

            {results.length > 0 && (
              <div className="text-sm text-muted-foreground">
                找到 {results.length} 个结果 {took !== null && `(耗时: ${took}ms)`}
              </div>
            )}

            <div className="space-y-4">
              {results.map((result, index) => (
                <Card key={index}>
                  <CardContent className="pt-6">
                    <div className="flex justify-between items-start mb-2">
                      <div className="font-semibold">ID: {result.document.id}</div>
                      <div className="text-sm text-muted-foreground">
                        相似度: {(result.score * 100).toFixed(2)}%
                      </div>
                    </div>
                    <JsonViewer data={result.document.data} />
                  </CardContent>
                </Card>
              ))}
            </div>

            {results.length === 0 && !loading && (queryText || queryVector) && (
              <div className="text-center py-8 text-muted-foreground">
                没有找到匹配的文档
              </div>
            )}
          </div>
        </CardContent>
      </Card>
    </div>
  )
}

