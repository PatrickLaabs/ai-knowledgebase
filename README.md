# AI Knowledgebase

A lightweight, personal AI-powered knowledgebase hacked on a weekend.
This project allows you to index local documents or notes and query them using an LLM and text embeddings for relevant context retrieval.

## Disclaimer:
I built it mainly based on my personal needs and desires, and I am running it on my
Kubernetes Environment with some tooling already in place (cnpg, valkey, you name it..).
Docs are coming, mainly focused on kubernetes and maybe docker-compose for local usages.

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

```mermaid
sequenceDiagram
    autonumber
    actor User
    participant App as Go Application
    participant Valkey as Valkey (External)
    participant PG as Postgres (External)
    participant Ollama as Ollama (External)

    %% -------------------------------------
    %% FLOW 1: DOCUMENT INDEXING
    %% -------------------------------------
    rect rgb(230, 240, 255)
    Note over User, Ollama: PHASE 1: Document Indexing (Ingestion)
    
    User->>App: Add/Upload new Markdown Note
    
    App->>Ollama: Send note text (Embedding request)
    Ollama-->>App: Return vector representation
    
    App->>PG: Store raw text + vector embedding
    App->>Valkey: Update cache with new metadata/indexing state
    
    App-->>User: "Note saved and indexed"
    end

    %% -------------------------------------
    %% FLOW 2: AI CHAT / SEARCH
    %% -------------------------------------
    rect rgb(235, 255, 235)
    Note over User, Ollama: PHASE 2: Chat & Search (Retrieval-Augmented Generation)
    
    User->>App: Ask a question
    
    App->>Valkey: Check cache for recent identical queries
    App->>Ollama: Send question text (Embedding request)
    Ollama-->>App: Return query vector
    
    App->>PG: Perform similarity search (pgvector)
    PG-->>App: Return top relevant notes/context
    
    App->>Ollama: Send System Prompt + Relevant Notes + User Question (Chat request)
    Ollama-->>App: Stream LLM answer
    
    App-->>User: Display context-aware answer
    end

```
