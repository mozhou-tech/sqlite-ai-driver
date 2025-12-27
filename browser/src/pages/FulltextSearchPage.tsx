import { useState } from 'react'
import { apiClient, FulltextSearchResult } from '../utils/api'
import { Button } from '../components/ui/Button'
import { Input } from '../components/ui/Input'
import { Card, CardContent, CardHeader, CardTitle } from '../components/ui/Card'
import { JsonViewer } from '../components/JsonViewer'

export default function FulltextSearchPage() {
  const [collection, setCollection] = useState('articles')
  const [query, setQuery] = useState('')
  const [results, setResults] = useState<FulltextSearchResult[]>([])
  const [took, setTook] = useState<number | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [limit, setLimit] = useState(10)

  const handleSearch = async () => {
    if (!query.trim() || !collection.trim()) {
      setError('请输入集合名称和搜索关键词')
      return
    }

    setLoading(true)
    setError(null)
    setTook(null)
    try {
      const response = await apiClient.fulltextSearch(
        collection,
        query,
        limit
      )
      setResults(response.results)
      setTook(response.took)
    } catch (err: any) {
      setError(err.message || '搜索失败')
      setResults([])
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="space-y-6">
      <Card>
        <CardHeader>
          <CardTitle>全文搜索</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="space-y-4">
            <div className="space-y-2">
              <label className="block text-sm font-medium">集合名称</label>
              <select
                className="w-full px-3 py-2 border rounded-md bg-background"
                value={collection}
                onChange={(e) => setCollection(e.target.value)}
              >
                <option value="articles">articles (seed 数据，支持全文搜索)</option>
                <option value="products">products (largeseed 数据)</option>
              </select>
              <p className="text-xs text-muted-foreground">
                提示: 全文搜索功能需要在集合上创建全文搜索索引，目前 <code className="px-1 py-0.5 bg-muted rounded">articles</code> 集合已配置全文搜索
              </p>
            </div>
            <div className="flex gap-4">
              <Input
                placeholder="搜索关键词"
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                onKeyPress={(e) => e.key === 'Enter' && handleSearch()}
                className="flex-2"
              />
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
                        相关性: {(result.score * 100).toFixed(2)}%
                      </div>
                    </div>
                    <JsonViewer data={result.document.data} />
                  </CardContent>
                </Card>
              ))}
            </div>

            {results.length === 0 && !loading && query && (
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

