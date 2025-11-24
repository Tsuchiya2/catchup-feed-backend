-- ============================================================
-- Seed Data for sources table
-- RSS/Atom feeds for web engineers and AI coders
-- ============================================================

-- Insert sources with ON CONFLICT to prevent duplicates
INSERT INTO sources (name, feed_url, source_type, scraper_config, active) VALUES
-- RSS/Atom sources (existing)
('Golang Weekly', 'https://cprss.s3.amazonaws.com/golangweekly.com.xml', 'RSS', NULL, TRUE),
('Ruby Weekly', 'https://cprss.s3.amazonaws.com/rubyweekly.com.xml', 'RSS', NULL, TRUE),
('Cloudflare Blog', 'https://blog.cloudflare.com/rss/', 'RSS', NULL, TRUE),
('GitHub Engineering Blog', 'https://github.blog/engineering/feed/', 'RSS', NULL, TRUE),
('Smashing Magazine', 'https://www.smashingmagazine.com/feed/', 'RSS', NULL, TRUE),
('DEV.to (Top Feed)', 'https://dev.to/feed/', 'RSS', NULL, TRUE),
('Anthropic (Claude Blog)', 'https://www.anthropic.com/feed.xml', 'RSS', NULL, FALSE),
('OpenAI Blog', 'https://openai.com/blog/rss/', 'RSS', NULL, FALSE),
('NVIDIA Developer Blog', 'https://developer.nvidia.com/blog/feed', 'RSS', NULL, TRUE),
('Hugging Face Blog', 'https://huggingface.co/blog/feed.xml', 'RSS', NULL, TRUE),
('VentureBeat AI – AI Section', 'https://venturebeat.com/category/ai/feed/', 'RSS', NULL, TRUE),
('AWS Machine Learning Blog', 'https://aws.amazon.com/blogs/machine-learning/feed/', 'RSS', NULL, TRUE),
('Google Cloud Blog (Unofficial RSS)', 'https://cloudblog.withgoogle.com/rss/', 'RSS', NULL, TRUE),
('Microsoft Azure AI Blog', 'https://azure.microsoft.com/en-us/blog/feed/', 'RSS', NULL, TRUE),
('JetBrains Blog', 'https://blog.jetbrains.com/feed/', 'RSS', NULL, TRUE),
('React Blog', 'https://react.dev/blog/rss.xml', 'RSS', NULL, TRUE),
('Vercel Blog', 'https://vercel.com/blog/rss', 'RSS', NULL, TRUE),
('Docker Blog', 'https://www.docker.com/blog/feed/', 'RSS', NULL, TRUE),
('Kubernetes Blog', 'https://kubernetes.io/feed.xml', 'RSS', NULL, TRUE),
('HashiCorp Blog', 'https://www.hashicorp.com/blog/feed.xml', 'RSS', NULL, TRUE),
('PostgreSQL News', 'https://www.postgresql.org/news.rss', 'RSS', NULL, TRUE),
('The Hacker News', 'https://feeds.feedburner.com/TheHackersNews', 'RSS', NULL, TRUE),
('OWASP Blog', 'https://owasp.org/feed.xml', 'RSS', NULL, TRUE),
('Publickey', 'https://www.publickey1.jp/atom.xml', 'RSS', NULL, TRUE),
('CodeZine', 'https://codezine.jp/rss/new/20/index.xml', 'RSS', NULL, TRUE),
('ICS MEDIA', 'https://ics.media/feed', 'RSS', NULL, TRUE),
('Qiita – Go', 'https://qiita.com/tags/go/feed.atom', 'RSS', NULL, TRUE),
('Qiita – Ruby', 'https://qiita.com/tags/ruby/feed.atom', 'RSS', NULL, TRUE),
('Qiita – TypeScript', 'https://qiita.com/tags/typescript/feed.atom', 'RSS', NULL, TRUE),
('Qiita – AI', 'https://qiita.com/tags/ai/feed.atom', 'RSS', NULL, TRUE),
('Qiita – Claude', 'https://qiita.com/tags/claude/feed.atom', 'RSS', NULL, TRUE),
('Qiita – LLM', 'https://qiita.com/tags/llm/feed.atom', 'RSS', NULL, TRUE),
('Zenn – Go', 'https://zenn.dev/topics/go/feed', 'RSS', NULL, TRUE),
('Zenn – Ruby', 'https://zenn.dev/topics/ruby/feed', 'RSS', NULL, TRUE),
('Zenn – TypeScript', 'https://zenn.dev/topics/typescript/feed', 'RSS', NULL, TRUE),
('Zenn – React', 'https://zenn.dev/topics/react/feed', 'RSS', NULL, TRUE),
('Zenn – AI', 'https://zenn.dev/topics/ai/feed', 'RSS', NULL, TRUE),
('Zenn – Claude', 'https://zenn.dev/topics/claude/feed', 'RSS', NULL, TRUE),
-- Web Scraper sources (Webflow)
('Claude Blog', 'https://www.claude.com/blog', 'Webflow', '{"item_selector":".blog_cms_item","title_selector":".card_blog_title","date_selector":".card_blog_list_field","link_selector":"a","date_format":"January 2, 2006"}', FALSE),
('Anthropic Events', 'https://www.anthropic.com/events', 'Webflow', '{"item_selector":".event_list_item","title_selector":".cc-name","date_selector":".cc-date","link_selector":"a","date_format":"Jan 2, 2006"}', FALSE),
-- Web Scraper sources (Next.js)
('Anthropic News', 'https://www.anthropic.com/news', 'NextJS', '{"data_key":"initialSeedData","url_prefix":"https://www.anthropic.com/news/"}', FALSE),
('Anthropic Engineering', 'https://www.anthropic.com/engineering', 'NextJS', '{"data_key":"initialSeedData","url_prefix":"https://www.anthropic.com/engineering/"}', FALSE),
-- Web Scraper sources (Remix)
('Python Weekly', 'https://www.pythonweekly.com', 'Remix', '{"context_key":"routes/($lang)._layout._index","url_prefix":"https://www.pythonweekly.com/issues/"}', FALSE)
ON CONFLICT (feed_url) DO NOTHING;
