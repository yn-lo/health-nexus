# api_key和接入模板

## llm agnes

sk-zsve4HntZRgy1heyMwGQt46TMMIovTZxhFdPBBDzgHi87T7k
curl https://apihub.agnes-ai.com/v1/chat/completions \
-H "Authorization: Bearer YOUR_API_KEY" \
-H "Content-Type: application/json" \
-d '{
    "model": "agnes-2.0-flash",
    "messages": [
      {
        "role": "user",
        "content": "你好！"
      }
    ]
  }'

## embedding 硅基流动
sk-sgmetwnlnkuyihibxejjphymsbuajfatrjcegwqahqixrnjz
curl -X POST https://api.siliconflow.cn/v1/embeddings \
  -H "Authorization: Bearer $SILICONFLOW_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "input": "Hello, world!",
    "model": "BAAI/bge-m3"
  }'



## rerank 硅基流动
sk-sgmetwnlnkuyihibxejjphymsbuajfatrjcegwqahqixrnjz
curl -X POST https://api.siliconflow.cn/v1/rerank \
  -H "Authorization: Bearer $SILICONFLOW_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "BAAI/bge-reranker-v2-m3",
    "query": "Apple",
    "documents": ["apple", "banana", "fruit", "vegetable"],
    "return_documents": true,
    "top_n": 4
  }'


  ## llm 小模型供理解用户意图：智谱
  f5edc4a3293e403dbeb41d09d874ef38.affxYCnYXAGdzK1D
  curl -X POST "https://open.bigmodel.cn/api/paas/v4/chat/completions" \
-H "Content-Type: application/json" \
-H "Authorization: Bearer YOUR_API_KEY" \
-d '{
    "model": "glm-4.7-flash",
    "messages": [
        {
            "role": "system",
            "content": "你是一个有用的AI助手。"
        },
        {
            "role": "user",
            "content": "你好，请介绍一下自己。"
        }
    ],
    "temperature": 1.0,
    "stream": true
}'
