-- ============================================================
-- Seed Data for sources table (pulse Phase 1 schema, §4)
--
-- §9: 旧 DB からはソース定義のみ手動移植。このファイルがその移植先。
-- 旧シードのうち Webflow / NextJS / Remix のスクレイパー依存ソースは
-- 新スキーマに設定カラムがないため落とした(全行 inactive だった)。
--
-- category は台本のコーナー分け単位(§4)。初期区分は
-- dev / ai / infra / security / community の5つ(要レビュー、
-- ダッシュボードからいつでも変更可能)。
-- ============================================================

INSERT INTO sources (name, feed_url, category, lang, active) VALUES
-- 開発・言語
('Golang Weekly', 'https://cprss.s3.amazonaws.com/golangweekly.com.xml', 'dev', 'en', TRUE),
('Ruby Weekly', 'https://cprss.s3.amazonaws.com/rubyweekly.com.xml', 'dev', 'en', TRUE),
('GitHub Engineering Blog', 'https://github.blog/engineering/feed/', 'dev', 'en', TRUE),
('Smashing Magazine', 'https://www.smashingmagazine.com/feed/', 'dev', 'en', TRUE),
('JetBrains Blog', 'https://blog.jetbrains.com/feed/', 'dev', 'en', TRUE),
('React Blog', 'https://react.dev/blog/rss.xml', 'dev', 'en', TRUE),
('Vercel Blog', 'https://vercel.com/blog/rss', 'dev', 'en', TRUE),
-- AI
('Anthropic (Claude Blog)', 'https://www.anthropic.com/feed.xml', 'ai', 'en', FALSE),
('OpenAI Blog', 'https://openai.com/blog/rss/', 'ai', 'en', FALSE),
('NVIDIA Developer Blog', 'https://developer.nvidia.com/blog/feed', 'ai', 'en', TRUE),
('Hugging Face Blog', 'https://huggingface.co/blog/feed.xml', 'ai', 'en', TRUE),
('VentureBeat AI – AI Section', 'https://venturebeat.com/category/ai/feed/', 'ai', 'en', TRUE),
('AWS Machine Learning Blog', 'https://aws.amazon.com/blogs/machine-learning/feed/', 'ai', 'en', TRUE),
('Microsoft Azure AI Blog', 'https://azure.microsoft.com/en-us/blog/feed/', 'ai', 'en', TRUE),
-- インフラ・クラウド
('Cloudflare Blog', 'https://blog.cloudflare.com/rss/', 'infra', 'en', TRUE),
('Google Cloud Blog (Unofficial RSS)', 'https://cloudblog.withgoogle.com/rss/', 'infra', 'en', TRUE),
('Docker Blog', 'https://www.docker.com/blog/feed/', 'infra', 'en', TRUE),
('Kubernetes Blog', 'https://kubernetes.io/feed.xml', 'infra', 'en', TRUE),
('HashiCorp Blog', 'https://www.hashicorp.com/blog/feed.xml', 'infra', 'en', TRUE),
('PostgreSQL News', 'https://www.postgresql.org/news.rss', 'infra', 'en', TRUE),
-- セキュリティ
('The Hacker News', 'https://feeds.feedburner.com/TheHackersNews', 'security', 'en', TRUE),
('OWASP Blog', 'https://owasp.org/feed.xml', 'security', 'en', TRUE),
-- コミュニティ(日本語含む)
('DEV.to (Top Feed)', 'https://dev.to/feed/', 'community', 'en', TRUE),
('Publickey', 'https://www.publickey1.jp/atom.xml', 'community', 'ja', TRUE),
('CodeZine', 'https://codezine.jp/rss/new/20/index.xml', 'community', 'ja', TRUE),
('ICS MEDIA', 'https://ics.media/feed', 'community', 'ja', TRUE),
('Qiita – Go', 'https://qiita.com/tags/go/feed.atom', 'community', 'ja', TRUE),
('Qiita – Ruby', 'https://qiita.com/tags/ruby/feed.atom', 'community', 'ja', TRUE),
('Qiita – TypeScript', 'https://qiita.com/tags/typescript/feed.atom', 'community', 'ja', TRUE),
('Qiita – AI', 'https://qiita.com/tags/ai/feed.atom', 'community', 'ja', TRUE),
('Qiita – Claude', 'https://qiita.com/tags/claude/feed.atom', 'community', 'ja', TRUE),
('Qiita – LLM', 'https://qiita.com/tags/llm/feed.atom', 'community', 'ja', TRUE),
('Zenn – Go', 'https://zenn.dev/topics/go/feed', 'community', 'ja', TRUE),
('Zenn – Ruby', 'https://zenn.dev/topics/ruby/feed', 'community', 'ja', TRUE),
('Zenn – TypeScript', 'https://zenn.dev/topics/typescript/feed', 'community', 'ja', TRUE),
('Zenn – React', 'https://zenn.dev/topics/react/feed', 'community', 'ja', TRUE),
('Zenn – AI', 'https://zenn.dev/topics/ai/feed', 'community', 'ja', TRUE),
('Zenn – Claude', 'https://zenn.dev/topics/claude/feed', 'community', 'ja', TRUE)
ON CONFLICT (feed_url) DO NOTHING;
