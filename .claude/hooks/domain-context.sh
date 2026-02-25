#!/bin/bash
# SessionStart hook: Load domain docs and inject ubiquitous language guidance
# This hook reads all domain docs from .utopia/domain/ and formats them
# as "use X not Y" guidance for consistent terminology usage.

set -e

DOMAIN_DIR=".utopia/domain"

# Check if domain directory exists
if [ ! -d "$DOMAIN_DIR" ]; then
    exit 0
fi

# Check if there are any yaml files
shopt -s nullglob
yaml_files=("$DOMAIN_DIR"/*.yaml)
shopt -u nullglob

if [ ${#yaml_files[@]} -eq 0 ]; then
    exit 0
fi

echo "## Domain Language Guide"
echo ""
echo "Use consistent terminology from the project's ubiquitous language:"
echo ""

# Simple YAML parser using awk
# This handles the specific structure of domain docs
parse_domain_doc() {
    local file="$1"
    awk '
    BEGIN {
        in_terms = 0
        in_entities = 0
        in_term = 0
        in_aliases = 0
        in_relationships = 0
        in_relationship = 0
        title = ""
        current_term = ""
        current_definition = ""
        current_aliases = ""
        entity_name = ""
        rel_type = ""
        rel_target = ""
    }

    /^title:/ {
        gsub(/^title:[ \t]*/, "")
        gsub(/["'\'']/, "")
        title = $0
        next
    }

    /^terms:/ {
        in_terms = 1
        in_entities = 0
        next
    }

    /^entities:/ {
        in_entities = 1
        in_terms = 0
        # Print section header before entities
        if (title != "" && terms_printed) {
            print ""
        }
        next
    }

    # Handle term entry
    in_terms && /^  - term:/ {
        # Print previous term if exists
        if (current_term != "") {
            if (current_aliases != "") {
                printf "- Use **%s** (not %s)\n", current_term, current_aliases
            } else {
                printf "- Use **%s**\n", current_term
            }
            # Truncate definition to 150 chars
            def = current_definition
            gsub(/\n/, " ", def)
            gsub(/  +/, " ", def)
            if (length(def) > 150) {
                def = substr(def, 1, 150) "..."
            }
            printf "  Meaning: %s\n", def
        }
        # Reset for new term
        gsub(/^  - term:[ \t]*/, "")
        current_term = $0
        current_definition = ""
        current_aliases = ""
        in_term = 1
        in_aliases = 0
        next
    }

    in_terms && in_term && /^    definition:/ {
        gsub(/^    definition:[ \t]*/, "")
        gsub(/\|$/, "")
        gsub(/^[ \t]*/, "")
        if ($0 != "" && $0 != "|") {
            current_definition = $0
        }
        in_definition = 1
        in_aliases = 0
        next
    }

    # Multiline definition continuation - stop if we hit aliases
    in_terms && in_term && in_definition && /^      [^ -]/ {
        gsub(/^      /, "")
        current_definition = current_definition " " $0
        next
    }

    in_terms && in_term && /^    aliases:/ {
        in_aliases = 1
        in_definition = 0
        next
    }

    in_terms && in_aliases && /^      - / {
        gsub(/^      - /, "")
        gsub(/["'\'']/, "")
        if (current_aliases == "") {
            current_aliases = "\"" $0 "\""
        } else {
            current_aliases = current_aliases ", \"" $0 "\""
        }
        next
    }

    # Handle entity entry
    in_entities && /^  - name:/ {
        gsub(/^  - name:[ \t]*/, "")
        entity_name = $0
        in_relationships = 0
        next
    }

    in_entities && /^    relationships:/ {
        in_relationships = 1
        next
    }

    in_entities && in_relationships && /^      - type:/ {
        gsub(/^      - type:[ \t]*/, "")
        rel_type = $0
        next
    }

    in_entities && in_relationships && /^        target:/ {
        gsub(/^        target:[ \t]*/, "")
        rel_target = $0
        if (entity_name != "" && rel_type != "" && rel_target != "") {
            relationships[entity_name "-" rel_type "-" rel_target] = entity_name " " rel_type " " rel_target
        }
        next
    }

    END {
        # Print last term
        if (current_term != "") {
            if (current_aliases != "") {
                printf "- Use **%s** (not %s)\n", current_term, current_aliases
            } else {
                printf "- Use **%s**\n", current_term
            }
            def = current_definition
            gsub(/\n/, " ", def)
            gsub(/  +/, " ", def)
            if (length(def) > 150) {
                def = substr(def, 1, 150) "..."
            }
            printf "  Meaning: %s\n", def
        }

        # Output title and relationships for later aggregation
        if (title != "") {
            print "TITLE:" title
        }
        for (key in relationships) {
            print "REL:" relationships[key]
        }
    }
    ' "$file"
}

# Collect all outputs
all_terms=""
all_relationships=""
current_section=""

for doc in "${yaml_files[@]}"; do
    output=$(parse_domain_doc "$doc")

    # Extract title
    title=$(echo "$output" | grep "^TITLE:" | sed 's/^TITLE://')

    # Extract terms (lines not starting with TITLE: or REL:)
    terms=$(echo "$output" | grep -v "^TITLE:" | grep -v "^REL:")

    # Extract relationships
    rels=$(echo "$output" | grep "^REL:" | sed 's/^REL://')

    if [ -n "$title" ] && [ -n "$terms" ]; then
        echo "### $title"
        echo "$terms"
        echo ""
    fi

    if [ -n "$rels" ]; then
        all_relationships="$all_relationships$rels"$'\n'
    fi
done

# Print entity relationships
if [ -n "$all_relationships" ]; then
    echo "### Entity Relationships"
    echo "$all_relationships" | grep -v "^$" | while read -r line; do
        echo "- $line"
    done
    echo ""
fi
