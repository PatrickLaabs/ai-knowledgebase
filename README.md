# AI Knowledgebase

A lightweight, personal AI-powered knowledgebase hacked on a weekend.
This project allows you to index local documents or notes and query them using an LLM and text embeddings for relevant context retrieval.

## Disclaimer:
I built it mainly based on my personal needs and desires, and I am running it on my
Kubernetes Environment with some tooling already in place (cnpg, valkey, you name it..).
Docs are coming, mainly focused on kubernetes and maybe docker-compose for local usages.

![Overview](./assets/overview.png)

## 🚀 Features

* **Document Indexing:** Easily parse and ingest markdown notes.
* **Vector Embeddings:** Automatically converts your knowledge into searchable vector embeddings.
* **AI Chat/Search:** Ask questions in plain English and get context-aware answers directly from your documents.
* **Lightweight & Fast:** Minimal setup designed for localized, personal use.

## 🛠️ Tech Stack

* **Language:** Go - well, and HTML.
* **LLM / Embeddings Backend:** Currently only: Ollama (e.g., `granite4.1` or `llama3`)
* **Vector Store:** Currently PSQL and Valkey

![AI-KnowledgeBase](assets/app-image.png "AI-KnowledgeBase")

## Data Flow

### Architecture Overview

```mermaid
graph TD
    U([User]) -->|HTTP + session cookie| MW[loggingMiddleware\nrequireAuth]
    MW --> APP{Go Application\nnotes · chat · auth · reindex}

    APP -->|embedText| OL[(Ollama\ngranite4.1 / llama3)]
    APP -->|pgvector store & search| PG[(PostgreSQL\npgvector)]
    APP -->|chat history TTL 30m| VK[(Valkey\nkey-value cache)]

    OL -->|vector float32| APP
    PG -->|top-5 notes| APP
    VK -->|last 10 turns| APP

    APP -->|SSE stream| U

    style APP fill:#534AB7,color:#EEEDFE,stroke:#3C3489
    style OL  fill:#993C1D,color:#FAECE7,stroke:#712B13
    style PG  fill:#185FA5,color:#E6F1FB,stroke:#0C447C
    style VK  fill:#BA7517,color:#FAEEDA,stroke:#854F0B
    style MW  fill:#5F5E5A,color:#F1EFE8,stroke:#444441
    style U   fill:#5F5E5A,color:#F1EFE8,stroke:#444441
```

---

### Phase 1 — Document Indexing

```mermaid
sequenceDiagram
    autonumber
    actor User
    participant App as Go Application
    participant Ollama as Ollama (embed model)
    participant PG as PostgreSQL (pgvector)
    participant VK as Valkey

    User->>App: POST /api/notes {content, tags}
    Note over App: notes.go → handleCreateNote

    App->>Ollama: embedText(content)
    Ollama-->>App: []float32 vector

    App->>PG: INSERT notes (content, tags, embedding, user_id)
    App->>VK: metadata / cache update

    App-->>User: 201 Created {id}
```

---

### Phase 2 — RAG Chat & Search

```mermaid
sequenceDiagram
    autonumber
    actor User
    participant App as Go Application
    participant VK as Valkey
    participant Ollama as Ollama (embed + generate)
    participant PG as PostgreSQL (pgvector)

    User->>App: POST /api/chat {query, session_id}
    Note over App: chat.go → handleChat

    App->>VK: LRANGE chat_history:{uid}:{session} 0 9
    VK-->>App: last 10 conversation turns

    App->>Ollama: embedText(query)
    Ollama-->>App: query vector []float32

    App->>PG: SELECT … ORDER BY embedding <=> $1 LIMIT 5
    PG-->>App: top-5 relevant notes

    Note over App: Build system prompt =<br/>system instructions<br/>+ retrieved notes<br/>+ conversation history

    App->>Ollama: Generate(systemPrompt + query) stream=true
    Ollama-->>App: streamed tokens

    App-->>User: SSE stream (data: {chunk, done})

    App->>VK: LPUSH + LTRIM + EXPIRE 30m (persist turn)
```
