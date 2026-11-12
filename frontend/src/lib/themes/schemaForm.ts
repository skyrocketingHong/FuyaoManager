/**
 * Schema-driven form fields for theme configuration: derives form controls
 * from a theme's declared configSchema (JSON Schema draft 2020-12 subset —
 * object properties of type string/number/boolean/enum). The generated form
 * and the JSON view share one state (the ThemeSelection store's
 * configText); this module only maps between parsed JSON and field values.
 */

export interface SchemaProperty {
	key: string;
	label: string;
	type: 'string' | 'number' | 'boolean' | 'enum';
	options?: string[];
	placeholder?: string;
	description?: string;
}

interface RawProperty {
	type?: string;
	title?: string;
	description?: string;
	enum?: unknown[];
	default?: unknown;
}

interface RawSchema {
	type?: string;
	properties?: Record<string, RawProperty>;
}

/** Extracts editable fields from a theme's config schema. */
export function fieldsFromSchema(schema: unknown): SchemaProperty[] {
	if (!schema || typeof schema !== 'object') return [];
	const raw = schema as RawSchema;
	if (raw.type && raw.type !== 'object') return [];
	const entries = Object.entries(raw.properties ?? {});
	const fields: SchemaProperty[] = [];
	for (const [key, prop] of entries) {
		if (!prop || typeof prop !== 'object') continue;
		const label = prop.title ?? key;
		const description = typeof prop.description === 'string' ? prop.description : undefined;
		if (Array.isArray(prop.enum) && prop.enum.length > 0) {
			fields.push({
				key,
				label,
				type: 'enum',
				options: prop.enum.map((v) => String(v)),
				description
			});
			continue;
		}
		switch (prop.type) {
			case 'string':
				fields.push({ key, label, type: 'string', description });
				break;
			case 'number':
			case 'integer':
				fields.push({ key, label, type: 'number', description });
				break;
			case 'boolean':
				fields.push({ key, label, type: 'boolean', description });
				break;
			default:
				// Unsupported shapes stay reachable through the JSON view.
				continue;
		}
	}
	return fields;
}

/**
 * Serializes field values back into the canonical config text. Values equal
 * to the schema default (or absent with a default) are normalized away so
 * the saved object stays minimal; unknown keys from the JSON view pass
 * through untouched.
 */
export function configTextFromFields(
	currentText: string,
	fields: SchemaProperty[],
	values: Record<string, unknown>
): string | null {
	let doc: Record<string, unknown>;
	try {
		doc = JSON.parse(currentText || '{}') as Record<string, unknown>;
	} catch {
		return null; // invalid JSON editing in progress — keep the textarea
	}
	for (const field of fields) {
		const value = values[field.key];
		if (value === undefined || value === '' || value === null) {
			delete doc[field.key];
			continue;
		}
		doc[field.key] = value;
	}
	return JSON.stringify(doc, null, 2);
}
