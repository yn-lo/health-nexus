function departmentSelector(config) {
    return {
        tree: Array.isArray(config.tree) ? config.tree : [],
        selectedDepts: config.selectedDepts || [],
        locked: config.locked || false,
        showUnlockConfirm: false,
        showPanel: false,
        groupedLeafNodes: [],

        init() {
            if (!Array.isArray(this.tree)) return;
            this._buildGroupedLeafNodes();
        },

        _buildGroupedLeafNodes() {
            var result = [];
            for (var i = 0; i < this.tree.length; i++) {
                var node = this.tree[i];
                var group = { id: node.id, name: node.name, is_public: node.is_public, leaves: [] };

                this._collectLeafNodes(node, group.leaves);

                if (group.leaves.length > 0) {
                    result.push(group);
                }
            }
            this.groupedLeafNodes = result;
        },

        _collectLeafNodes(node, leaves) {
            if (!node.children || node.children.length === 0) {
                leaves.push({ id: node.id, name: node.name, is_public: node.is_public });
                return;
            }
            for (var i = 0; i < node.children.length; i++) {
                this._collectLeafNodes(node.children[i], leaves);
            }
        },

        toggleDept(deptId) {
            if (this.locked) return;
            var value = String(deptId);
            var idx = this.selectedDepts.indexOf(value);
            if (idx > -1) {
                this.selectedDepts.splice(idx, 1);
            } else {
                this.selectedDepts.push(value);
            }
        },

        isDeptSelected(deptId) {
            return this.selectedDepts.includes(String(deptId));
        },

        requestUnlock() {
            this.showUnlockConfirm = true;
        },

        confirmUnlock() {
            this.locked = false;
            this.showUnlockConfirm = false;
            this.showPanel = true;
        },

        cancelUnlock() {
            this.showUnlockConfirm = false;
        },

        togglePanel() {
            if (this.locked) {
                this.requestUnlock();
                return;
            }
            this.showPanel = !this.showPanel;
        },

        get selectedDeptNames() {
            var allDepts = this._flattenTree(this.tree);
            return allDepts
                .filter(function(d) { return this.selectedDepts.includes(String(d.id)); }.bind(this))
                .map(function(d) { return d.name; });
        },

        get isModified() {
            return JSON.stringify(this.selectedDepts.sort()) !== JSON.stringify((config.originalDepts || []).sort());
        },

        _flattenTree(nodes) {
            if (!Array.isArray(nodes)) return [];
            var result = [];
            nodes.forEach(function(node) {
                result.push(node);
                if (node.children && node.children.length > 0) {
                    result = result.concat(this._flattenTree(node.children));
                }
            }.bind(this));
            return result;
        },

        flattenTree(nodes) {
            return this._flattenTree(nodes);
        }
    };
}
