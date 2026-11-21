"""Extract paperless-ngx's workflow serializer fields for the CUE sync test.

Run against a paperless-ngx checkout when upgrading the supported version:

    python3 -I testdata/upstream/extract_workflow_fields.py /path/to/paperless-ngx \
        > testdata/upstream/workflow_serializers_<version>.json

It reads src/documents/serialisers.py and src/documents/models.py with the ast
module (nothing is imported or run) and records, for each workflow
serializer, its writable fields and which model fields allow null, plus the
choice values of the enums the CUE schema names.
"""

import ast
import json
import sys
from pathlib import Path

SERIALIZERS = {
    "WorkflowSerializer": "Workflow",
    "WorkflowTriggerSerializer": "WorkflowTrigger",
    "WorkflowActionSerializer": "WorkflowAction",
    "WorkflowActionEmailSerializer": "WorkflowActionEmail",
    "WorkflowActionWebhookSerializer": "WorkflowActionWebhook",
}
CHOICES = {
    "trigger_type": ("WorkflowTrigger", "WorkflowTriggerType"),
    "matching_algorithm": ("WorkflowTrigger", "WorkflowTriggerMatching"),
    "source": ("WorkflowTrigger", "DocumentSourceChoices"),
    "schedule_date_field": ("WorkflowTrigger", "ScheduleDateField"),
    "action_type": ("WorkflowAction", "WorkflowActionType"),
    "ai_suggestion_field": ("WorkflowAction", "AISuggestionField"),
}
# Values the choices classes take from elsewhere.
KNOWN = {
    "MatchingModel.MATCH_NONE": 0, "MatchingModel.MATCH_ANY": 1,
    "MatchingModel.MATCH_ALL": 2, "MatchingModel.MATCH_LITERAL": 3,
    "MatchingModel.MATCH_REGEX": 4, "MatchingModel.MATCH_FUZZY": 5,
    "DocumentSource.ConsumeFolder.value": 1, "DocumentSource.ApiUpload.value": 2,
    "DocumentSource.MailFetch.value": 3, "DocumentSource.WebUI.value": 4,
}


def classes(tree):
    return {n.name: n for n in ast.walk(tree) if isinstance(n, ast.ClassDef)}


def meta_fields(cls):
    for node in cls.body:
        if isinstance(node, ast.ClassDef) and node.name == "Meta":
            for stmt in node.body:
                if isinstance(stmt, ast.Assign) and stmt.targets[0].id == "fields":
                    return [e.value for e in stmt.value.elts]
    return []


def nullable_fields(cls):
    out = []
    for stmt in cls.body:
        if isinstance(stmt, ast.Assign) and isinstance(stmt.value, ast.Call):
            for kw in stmt.value.keywords:
                if kw.arg == "null" and isinstance(kw.value, ast.Constant) and kw.value.value:
                    out.append(stmt.targets[0].id)
    return sorted(out)


def choice_values(cls):
    values = []
    for stmt in cls.body:
        if not isinstance(stmt, ast.Assign):
            continue
        v = stmt.value
        if isinstance(v, ast.Tuple):
            v = v.elts[0]
        if isinstance(v, ast.Constant):
            values.append(v.value)
        else:
            values.append(KNOWN[ast.unparse(v)])
    return values


def main(root):
    src = Path(root) / "src" / "documents"
    ser = classes(ast.parse((src / "serialisers.py").read_text()))
    mod = classes(ast.parse((src / "models.py").read_text()))
    out = {"serializers": {}, "choices": {}}
    for name, model in SERIALIZERS.items():
        out["serializers"][name] = {
            "fields": meta_fields(ser[name]),
            "nullable": nullable_fields(mod[model]),
        }
    for key, (model, inner) in CHOICES.items():
        inner_cls = next(n for n in mod[model].body if isinstance(n, ast.ClassDef) and n.name == inner)
        out["choices"][key] = choice_values(inner_cls)
    json.dump(out, sys.stdout, indent=2, sort_keys=True)
    print()


if __name__ == "__main__":
    main(sys.argv[1])
