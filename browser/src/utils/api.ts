import axios from 'axios'

const API_URL = import.meta.env.VITE_API_URL || '/api'

const api = axios.create({
  baseURL: API_URL,
  headers: {
    'Content-Type': 'application/json',
  },
})

export interface Document {
  id: string
  data: Record<string, any>
}

export interface DocumentListResponse {
  documents: Document[]
  total: number
  skip: number
  limit: number
}

export interface FulltextSearchRequest {
  collection: string
  query: string
  limit?: number
  threshold?: number
}

export interface FulltextSearchResult {
  document: Document
  score: number
}

export interface VectorSearchRequest {
  collection: string
  query?: number[]      // 向量查询（可选）
  query_text?: string  // 文本查询（可选，将自动生成 embedding）
  limit?: number
  field?: string
}

export interface VectorSearchResult {
  document: Document
  score: number
}

export interface GraphLinkRequest {
  from: string
  relation: string
  to: string
}

export interface GraphNeighborsResponse {
  node_id: string
  relation: string
  neighbors: string[]
}

export interface GraphPathRequest {
  from: string
  to: string
  max_depth?: number
  relations?: string[]
}

export interface GraphPathResponse {
  from: string
  to: string
  paths: string[][]
}

export interface GraphQueryRequest {
  query: string
}

export interface GraphQueryResult {
  subject: string
  predicate: string
  object: string
}

export interface GraphQueryResponse {
  query: string
  results: GraphQueryResult[]
}

export interface FulltextSearchResponse {
  results: FulltextSearchResult[]
  took: number
}

export interface VectorSearchResponse {
  results: VectorSearchResult[]
  took: number
}

export const apiClient = {
  // 获取集合列表
  getCollections: async (): Promise<string[]> => {
    const response = await api.get('/db/collections')
    return response.data.collections || []
  },

  // 获取文档列表
  getDocuments: async (
    collection: string,
    skip = 0,
    limit = 100,
    tag?: string
  ): Promise<DocumentListResponse> => {
    const params: Record<string, any> = { skip, limit }
    if (tag) {
      params.tag = tag
    }
    const response = await api.get(`/collections/${collection}/documents`, {
      params,
    })
    return response.data
  },

  // 获取单个文档
  getDocument: async (collection: string, id: string): Promise<Document> => {
    const response = await api.get(`/collections/${collection}/documents/${id}`)
    return response.data
  },

  // 创建文档
  createDocument: async (
    collection: string,
    data: Record<string, any>
  ): Promise<Document> => {
    const response = await api.post(`/collections/${collection}/documents`, data)
    return response.data
  },

  // 更新文档
  updateDocument: async (
    collection: string,
    id: string,
    updates: Record<string, any>
  ): Promise<Document> => {
    const response = await api.put(
      `/collections/${collection}/documents/${id}`,
      updates
    )
    return response.data
  },

  // 删除文档
  deleteDocument: async (collection: string, id: string): Promise<void> => {
    await api.delete(`/collections/${collection}/documents/${id}`)
  },

  // 全文搜索
  fulltextSearch: async (
    collection: string,
    query: string,
    limit = 10,
    threshold = 0
  ): Promise<FulltextSearchResponse> => {
    const response = await api.post(
      `/collections/${collection}/fulltext/search`,
      {
        collection,
        query,
        limit,
        threshold,
      }
    )
    return {
      results: response.data.results || [],
      took: response.data.took || 0
    }
  },

  // 向量搜索
  vectorSearch: async (
    collection: string,
    query?: number[],
    limit = 10,
    field = 'embedding',
    queryText?: string
  ): Promise<VectorSearchResponse> => {
    const requestBody: VectorSearchRequest = {
      collection,
      limit,
      field,
    }
    
    if (queryText) {
      requestBody.query_text = queryText
    } else if (query) {
      requestBody.query = query
    } else {
      throw new Error('Either query (vector) or queryText must be provided')
    }
    
    const response = await api.post(
      `/collections/${collection}/vector/search`,
      requestBody
    )
    return {
      results: response.data.results || [],
      took: response.data.took || 0
    }
  },

  // 图数据库操作
  // 创建图关系链接
  graphLink: async (from: string, relation: string, to: string): Promise<void> => {
    await api.post('/graph/link', { from, relation, to })
  },

  // 删除图关系链接
  graphUnlink: async (from: string, relation: string, to: string): Promise<void> => {
    await api.delete('/graph/link', { data: { from, relation, to } })
  },

  // 获取节点的邻居
  graphNeighbors: async (
    nodeId: string,
    relation?: string
  ): Promise<GraphNeighborsResponse> => {
    const params = relation ? { relation } : {}
    const response = await api.get(`/graph/neighbors/${nodeId}`, { params })
    return response.data
  },

  // 查找两个节点之间的路径
  graphPath: async (
    from: string,
    to: string,
    maxDepth = 5,
    relations?: string[]
  ): Promise<GraphPathResponse> => {
    const response = await api.post('/graph/path', {
      from,
      to,
      max_depth: maxDepth,
      relations,
    })
    return response.data
  },

  // 执行图查询
  graphQuery: async (query: string): Promise<GraphQueryResponse> => {
    const response = await api.post('/graph/query', { query })
    return response.data
  },
}

export default api

