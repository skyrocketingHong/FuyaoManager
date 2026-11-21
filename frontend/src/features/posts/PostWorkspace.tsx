import { useSearchParams } from 'react-router-dom';
import { queryChoice } from '../../lib/navigation/query';
import PostsPage from '../../pages/content/posts/PostsPage';
import PostVocabularyPage from '../../pages/content/posts/PostVocabularyPage';
import PostMediaPage from '../../pages/content/posts/PostMediaPage';

export default function PostWorkspace() {
	const [params] = useSearchParams();
	switch (queryChoice(params, 'tab', ['posts', 'categories', 'tags', 'media'], 'posts')) {
		case 'categories':
			return <PostVocabularyPage key="categories" kind="categories" />;
		case 'tags':
			return <PostVocabularyPage key="tags" kind="tags" />;
		case 'media':
			return <PostMediaPage />;
		default:
			return <PostsPage />;
	}
}
