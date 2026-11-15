import { Navigate, useNavigate, useParams, useSearchParams } from 'react-router-dom';
import { PageTabs } from '../../components/PageHeader';
import { t } from '../../lib/i18n';

export type PostWorkspaceSection = 'posts' | 'categories' | 'tags' | 'media';

export function postWorkspacePath(siteId?: string): string {
	return siteId ? `/sites/${encodeURIComponent(siteId)}/posts` : '/content/posts';
}

export function LegacyPostVocabularyRedirect() {
	const { siteId } = useParams<{ siteId: string }>();
	const [params] = useSearchParams();
	const section = params.get('view') === 'tags' ? 'tags' : 'categories';
	return <Navigate to={`${postWorkspacePath(siteId)}/${section}`} replace />;
}

export default function PostWorkspaceTabs({ current }: { current: PostWorkspaceSection }) {
	const { siteId } = useParams<{ siteId: string }>();
	const navigate = useNavigate();
	const base = postWorkspacePath(siteId);
	return (
		<PageTabs
			value={current}
			ariaLabel={t('posts.workspace')}
			options={[
				{ value: 'posts', label: t('posts.articles') },
				{ value: 'categories', label: t('taxonomy.categories') },
				{ value: 'tags', label: t('taxonomy.tags') },
				{ value: 'media', label: t('posts.media') }
			]}
			onChange={(value) => {
				if (value === 'posts') navigate(base);
				else if (value === 'media') navigate(`${base}/media`);
				else navigate(`${base}/${value}`);
			}}
		/>
	);
}
