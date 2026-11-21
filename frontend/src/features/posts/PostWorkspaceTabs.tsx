import { Navigate, useLocation, useNavigate, useParams, useSearchParams } from 'react-router-dom';
import { PageTabs } from '../../components/PageHeader';
import { t } from '../../lib/i18n';
import { tabDestination } from '../../lib/navigation/query';

export type PostWorkspaceSection = 'posts' | 'categories' | 'tags' | 'media';

export function postWorkspacePath(siteId?: string): string {
	return siteId ? `/sites/${encodeURIComponent(siteId)}/posts` : '/content/posts';
}

export function LegacyPostSectionRedirect({ section }: { section: PostWorkspaceSection }) {
	const { siteId } = useParams<{ siteId: string }>();
	const { search, hash } = useLocation();
	return <Navigate to={{ ...tabDestination(postWorkspacePath(siteId), search, section), hash }} replace />;
}

export function LegacyPostVocabularyRedirect() {
	const [params] = useSearchParams();
	const section = params.get('view') === 'tags' ? 'tags' : 'categories';
	return <LegacyPostSectionRedirect section={section} />;
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
				navigate(value === 'posts' ? base : tabDestination(base, '', value));
			}}
		/>
	);
}
